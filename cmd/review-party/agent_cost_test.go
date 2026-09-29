package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

var updateAgentCost = flag.Bool("update", false, "rewrite testdata/agent-cost.tsv; add -v to see the before/after delta")

const (
	agentCostPath      = "testdata/agent-cost.tsv"
	agentCostPreamble  = "# est_tokens is an estimate: ceil((in_bytes+out_bytes)/4)\nsurface\texit\tin_bytes\tout_bytes\test_tokens\n"
	agentCostConfigDir = "/nonexistent/review-party-agent-cost/config"
	costCleanReview    = model.ReviewID("rp_1723200000000_00000000000c0000")
	costOneReview      = model.ReviewID("rp_1723200000000_00000000000c0001")
	costThreeReview    = model.ReviewID("rp_1723200000000_00000000000c0003")
	costEightReview    = model.ReviewID("rp_1723200000000_00000000000c0008")
	costMissingReview  = model.ReviewID("rp_1723200000000_00000000000cffff")
	costBundle         = model.ReviewBundleID("rb_1723200000000_00000000000b0003")
)

var costClock = time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)

// agentCostScenario is one command an agent types and the ledger it runs against.
// Its cost is what the agent spends: the command line and stdin it writes plus
// every byte the command prints back.
type agentCostScenario struct {
	surface string
	args    []string
	stdin   string
	seed    func(testing.TB, *store.LedgerRecordStore)
}

type agentCostRow struct {
	surface  string
	exit     int
	inBytes  int
	outBytes int
}

func (row agentCostRow) estimatedTokens() int {
	return (row.inBytes + row.outBytes + 3) / 4
}

func (row agentCostRow) String() string {
	return fmt.Sprintf("%s\t%d\t%d\t%d\t%d", row.surface, row.exit, row.inBytes, row.outBytes, row.estimatedTokens())
}

func costRecord(id model.ReviewID, profile string, findings int) model.ReviewRecord {
	record := largePatchRecord(id, profile, findings)
	record.SchemaVersion = model.CurrentReviewRecordSchemaVersion
	record.CreatedAt, record.UpdatedAt = costClock, costClock
	return record
}

func seedReviews(records ...model.ReviewRecord) func(testing.TB, *store.LedgerRecordStore) {
	return func(t testing.TB, ledger *store.LedgerRecordStore) {
		t.Helper()
		for _, record := range records {
			if err := ledger.Save(record); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func seedCostBundle(t testing.TB, ledger *store.LedgerRecordStore) {
	t.Helper()
	members := []model.ReviewRecord{costRecord(costThreeReview, "bugs", 3), costRecord(costCleanReview, "code-quality", 0), costRecord(costOneReview, "security", 1)}
	seedReviews(members...)(t, ledger)
	bundle := model.ReviewBundle{
		ID: costBundle, Revision: "bundle-revision", Repository: "/repo", SubjectKind: model.SubjectWorkingChanges, SubjectIdentity: "0123456789abcdef0123",
		Lifecycle: model.LifecycleCompleted, Warnings: []model.BundleWarning{}, Deduplicated: []model.SkippedDuplicate{},
		ConcurrencyLimit: 3, CreatedAt: costClock, UpdatedAt: costClock,
	}
	for _, record := range members {
		bundle.Members = append(bundle.Members, model.BundleMember{
			Scope: "global", Profile: record.ProfileRevision.Name, ReviewID: record.ID, Lifecycle: record.Lifecycle,
			Status: string(record.Result.Status), FindingCount: record.Result.FindingCount(),
		})
	}
	if err := ledger.CreateReviewBundle(bundle, nil); err != nil {
		t.Fatal(err)
	}
}

func agentCostScenarios() []agentCostScenario {
	var scenarios []agentCostScenario
	for _, review := range []struct {
		id       model.ReviewID
		findings int
	}{{costCleanReview, 0}, {costThreeReview, 3}, {costEightReview, 8}} {
		seed := seedReviews(costRecord(review.id, "bugs", review.findings))
		for _, format := range []string{"human", "json"} {
			scenarios = append(scenarios, agentCostScenario{
				surface: fmt.Sprintf("report.inspect.%s.findings-%d", format, review.findings),
				args:    []string{"inspect", string(review.id), "--format", format}, seed: seed,
			})
		}
	}
	threeFindings := seedReviews(costRecord(costThreeReview, "bugs", 3))
	for _, format := range []string{"human", "json"} {
		scenarios = append(scenarios, agentCostScenario{
			surface: "report.wait." + format + ".findings-3",
			args:    []string{"wait", string(costThreeReview), "--format", format}, seed: threeFindings,
		})
	}
	return append(scenarios,
		agentCostScenario{surface: "report.inspect.human.bundle-3", args: []string{"inspect", string(costBundle)}, seed: seedCostBundle},
		agentCostScenario{surface: "report.inspect.json.bundle-3", args: []string{"inspect", string(costBundle), "--format", "json"}, seed: seedCostBundle},
		agentCostScenario{surface: "help.root", args: []string{"--help"}},
		agentCostScenario{surface: "error.inspect.missing-review", args: []string{"inspect", string(costMissingReview)}, seed: threeFindings},
		agentCostScenario{surface: "error.inspect.no-argument", args: []string{"inspect"}},
	)
}

func measureAgentCost(t testing.TB, scenario agentCostScenario) agentCostRow {
	t.Helper()
	stateHome := prepareAgentCostState(t, scenario)
	var stdout, stderr bytes.Buffer
	exit := execute(context.Background(), scenario.args, productionCommandIO(strings.NewReader(scenario.stdin), &stdout, &stderr))
	if strings.Contains(stdout.String()+stderr.String(), stateHome) {
		t.Fatalf("%s output names the per-run state directory, so its bytes are not stable:\n%s%s", scenario.surface, stdout.String(), stderr.String())
	}
	command := strings.Join(append([]string{"review-party"}, scenario.args...), " ")
	return agentCostRow{surface: scenario.surface, exit: exit, inBytes: len(command) + len(scenario.stdin), outBytes: stdout.Len() + stderr.Len()}
}

// prepareAgentCostState gives each scenario fresh state and a fixed configuration
// path, since help text and command hints print the default configuration path.
func prepareAgentCostState(t testing.TB, scenario agentCostScenario) string {
	t.Helper()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", agentCostConfigDir)
	if scenario.seed == nil {
		return stateHome
	}
	ledger, err := store.NewLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	scenario.seed(t, ledger)
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	return stateHome
}

func TestAgentCost(t *testing.T) {
	scenarios := agentCostScenarios()
	rows := make([]agentCostRow, 0, len(scenarios))
	for _, scenario := range scenarios {
		rows = append(rows, measureAgentCost(t, scenario))
	}
	checkAgentCostBudgets(t, rows)
	if t.Failed() {
		return
	}
	var generated strings.Builder
	generated.WriteString(agentCostPreamble)
	for _, row := range rows {
		generated.WriteString(row.String() + "\n")
	}
	recorded := readAgentCost(t)
	if recorded == generated.String() {
		return
	}
	delta := agentCostDelta(parseAgentCost(t, recorded), rows)
	if !*updateAgentCost {
		t.Fatalf("agent cost changed; rerun with -update to accept it:\n%s", delta)
	}
	if err := os.WriteFile(agentCostPath, []byte(generated.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("agent cost updated:\n%s", delta)
}

// readAgentCost returns the recorded table, or an empty one before the first -update.
func readAgentCost(t *testing.T) string {
	t.Helper()
	recorded, err := os.ReadFile(agentCostPath)
	if os.IsNotExist(err) {
		return agentCostPreamble
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(recorded)
}

// agentCostBudgets holds the promises the gate keeps even under -update.
var agentCostBudgets = []struct {
	surface string
	promise string
	check   func(agentCostRow) bool
}{
	{"report.inspect.human.findings-0", "a clean review prints no feedback hint", func(row agentCostRow) bool { return row.outBytes == 276 }},
	{"report.inspect.json.findings-0", "a clean review prints no feedback hint", func(row agentCostRow) bool { return row.outBytes == 708 }},
}

func checkAgentCostBudgets(t *testing.T, rows []agentCostRow) {
	t.Helper()
	bySurface := make(map[string]agentCostRow, len(rows))
	for _, row := range rows {
		bySurface[row.surface] = row
	}
	for _, budget := range agentCostBudgets {
		row, ok := bySurface[budget.surface]
		if !ok || !budget.check(row) {
			t.Errorf("%s breaks its budget (%s): %+v", budget.surface, budget.promise, row)
		}
	}
}

func parseAgentCost(t *testing.T, text string) map[string]agentCostRow {
	t.Helper()
	body, ok := strings.CutPrefix(text, agentCostPreamble)
	if !ok {
		t.Fatalf("agent cost table does not start with %q", agentCostPreamble)
	}
	rows := map[string]agentCostRow{}
	for line := range strings.Lines(body) {
		var row agentCostRow
		var estimate int
		if _, err := fmt.Sscanf(line, "%s\t%d\t%d\t%d\t%d\n", &row.surface, &row.exit, &row.inBytes, &row.outBytes, &estimate); err != nil {
			t.Fatalf("malformed agent cost line %q: %v", line, err)
		}
		rows[row.surface] = row
	}
	return rows
}

// agentCostDelta lists every surface whose exit, bytes, or estimate moved.
func agentCostDelta(before map[string]agentCostRow, after []agentCostRow) string {
	var table strings.Builder
	fmt.Fprintf(&table, "%-40s %4s %10s %10s %7s %10s %10s\n", "surface", "exit", "out_before", "out_after", "delta", "tok_before", "tok_after")
	seen := map[string]bool{}
	for _, row := range after {
		seen[row.surface] = true
		old, existed := before[row.surface]
		switch {
		case !existed:
			fmt.Fprintf(&table, "%-40s %4d %10s %10d %+7d %10s %10d\n", row.surface+" (new)", row.exit, "-", row.outBytes, row.outBytes, "-", row.estimatedTokens())
		case old != row:
			fmt.Fprintf(&table, "%-40s %4d %10d %10d %+7d %10d %10d\n", row.surface, row.exit, old.outBytes, row.outBytes, row.outBytes-old.outBytes, old.estimatedTokens(), row.estimatedTokens())
		}
	}
	for surface, old := range before {
		if !seen[surface] {
			fmt.Fprintf(&table, "%-40s %4d %10d %10s %+7d %10d %10s\n", surface+" (gone)", old.exit, old.outBytes, "-", -old.outBytes, old.estimatedTokens(), "-")
		}
	}
	return table.String()
}

func benchmarkAgentSurface(b *testing.B, scenario agentCostScenario) {
	prepareAgentCostState(b, scenario)
	for b.Loop() {
		var stdout, stderr bytes.Buffer
		if exit := execute(context.Background(), scenario.args, productionCommandIO(strings.NewReader(scenario.stdin), &stdout, &stderr)); exit != 0 {
			b.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
		}
	}
}

func BenchmarkInspectThreeFindingsHuman(b *testing.B) {
	benchmarkAgentSurface(b, agentCostScenario{args: []string{"inspect", string(costThreeReview)}, seed: seedReviews(costRecord(costThreeReview, "bugs", 3))})
}

func BenchmarkInspectThreeFindingsJSON(b *testing.B) {
	benchmarkAgentSurface(b, agentCostScenario{args: []string{"inspect", string(costThreeReview), "--format", "json"}, seed: seedReviews(costRecord(costThreeReview, "bugs", 3))})
}
