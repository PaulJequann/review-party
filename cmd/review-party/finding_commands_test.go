package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const threeFindingHint = `feedback: review-party finding record ` + string(costThreeReview) + " <<'EOF'\nN accept|reject|defer REASON\nEOF\n"

func newFindingLedger(t *testing.T) string {
	t.Helper()
	return filepath.Join(prepareAgentCostState(t, agentCostScenario{seed: seedCostBundle}), "review-party")
}

type cliRun struct {
	exit   int
	stdout string
	stderr string
}

func findingCLI(stdin string, arguments ...string) cliRun {
	var stdout, stderr bytes.Buffer
	exit := execute(context.Background(), arguments, productionCommandIO(strings.NewReader(stdin), &stdout, &stderr))
	return cliRun{exit, stdout.String(), stderr.String()}
}

// succeedCLI requires the command to exit 0 with nothing on stderr and returns
// its stdout.
func succeedCLI(t *testing.T, stdin string, arguments ...string) string {
	t.Helper()
	run := findingCLI(stdin, arguments...)
	if run.exit != 0 || run.stderr != "" {
		t.Fatalf("%v exit = %d, stderr = %q, stdout =\n%s", arguments, run.exit, run.stderr, run.stdout)
	}
	return run.stdout
}

func requireFindingCLI(t *testing.T, wantStdout, stdin string, arguments ...string) {
	t.Helper()
	if stdout := succeedCLI(t, stdin, arguments...); stdout != wantStdout {
		t.Fatalf("%v stdout = %q, want %q", arguments, stdout, wantStdout)
	}
}

func inspectFindingVerdicts(t *testing.T, id model.ReviewID) ([]*verdictView, string) {
	t.Helper()
	var report reviewReport
	if err := json.Unmarshal([]byte(succeedCLI(t, "", "inspect", string(id), "--format", "json")), &report); err != nil {
		t.Fatal(err)
	}
	var verdicts []*verdictView
	for _, finding := range report.Reviews[0].Findings {
		verdicts = append(verdicts, finding.Verdict)
	}
	return verdicts, report.Feedback
}

func TestFindingRecordShowsVerdictsOnInspectUntilEveryFindingIsJudged(t *testing.T) {
	newFindingLedger(t)
	requireFindingCLI(t, "recorded 2\n", "1 accept nil map write is reachable\n\n2   reject  guarded upstream  \n", "finding", "record", string(costThreeReview))

	human := succeedCLI(t, "", "inspect", string(costThreeReview))
	judged := "     test: Exercise the caller with the new state.\n     accepted: nil map write is reachable\n  2. HIGH · correctness · internal/file02.go:2\n" +
		"     The changed state is not handled.\n     evidence: The caller ignores the new state.\n     fix: Handle the state in the caller.\n" +
		"     test: Exercise the caller with the new state.\n     rejected: guarded upstream\n  3. "
	if !strings.Contains(human, judged) {
		t.Fatalf("inspect stdout =\n%s\nwant the verdict under findings 1 and 2", human)
	}
	if !strings.HasSuffix(human, threeFindingHint) {
		t.Fatalf("inspect stdout =\n%s\nwant it to end with %q", human, threeFindingHint)
	}
	verdicts, feedback := inspectFindingVerdicts(t, costThreeReview)
	want := []*verdictView{{model.VerdictAccepted, "nil map write is reachable"}, {model.VerdictRejected, "guarded upstream"}, nil}
	if !reflect.DeepEqual(verdicts, want) {
		t.Fatalf("verdicts = %v, want %v", verdicts, want)
	}
	if feedback != strings.TrimSuffix(strings.TrimPrefix(threeFindingHint, "feedback: "), "\n") {
		t.Fatalf("feedback = %q", feedback)
	}

	requireFindingCLI(t, "recorded 2 (1 unchanged) (1 changed)\n", "1 accept nil map write is reachable\n2 defer revisit after the refactor\n3 accept real\n", "finding", "record", string(costThreeReview))
	human = succeedCLI(t, "", "inspect", string(costThreeReview))
	if strings.Contains(human, "feedback:") || !strings.Contains(human, "     deferred: revisit after the refactor\n") {
		t.Fatalf("inspect stdout =\n%s\nwant the changed verdict and no feedback hint", human)
	}
	if _, feedback := inspectFindingVerdicts(t, costThreeReview); feedback != "" {
		t.Fatalf("feedback = %q with every finding judged, want none", feedback)
	}
	requireFindingCLI(t, `{"recorded":0,"unchanged":1,"changed":0}`+"\n", "3 accept real", "finding", "record", string(costThreeReview), "--format", "json")
}

func TestInspectBundleHintNamesEachMemberWithAnUnjudgedFinding(t *testing.T) {
	newFindingLedger(t)
	hint := func(review model.ReviewID) string {
		return "review-party finding record " + string(review) + " <<'EOF'\nN accept|reject|defer REASON\nEOF"
	}
	if human := succeedCLI(t, "", "inspect", string(costBundle)); !strings.HasSuffix(human, "\nfeedback: "+hint(costThreeReview)+"\n"+hint(costOneReview)+"\n") {
		t.Fatalf("inspect stdout =\n%s\nwant the hint naming both unjudged members", human)
	}
	requireFindingCLI(t, "recorded 3\n", "1 accept a\n2 accept b\n3 accept c\n", "finding", "record", string(costThreeReview))
	if human := succeedCLI(t, "", "inspect", string(costBundle)); !strings.HasSuffix(human, "\nfeedback: "+hint(costOneReview)+"\n") {
		t.Fatalf("inspect stdout =\n%s\nwant the hint naming the one unjudged member", human)
	}
	requireFindingCLI(t, "recorded 1\n", "1 reject d\n", "finding", "record", string(costOneReview))
	if human := succeedCLI(t, "", "inspect", string(costBundle)); strings.Contains(human, "feedback:") {
		t.Fatalf("inspect stdout =\n%s\nwant no feedback hint", human)
	}
}

func TestFindingRecordRefusesBadInputAndRecordsNothing(t *testing.T) {
	newFindingLedger(t)
	record := []string{"finding", "record", string(costThreeReview)}
	for _, test := range []struct {
		stdin     string
		arguments []string
		exit      int
		stderr    string
	}{
		{"", record, 2, `stdin has no "N accept|reject|defer REASON" lines; nothing recorded`},
		{"x accept r", record, 2, `line 1: "x" is not a finding number; start the line with N from the report; nothing recorded`},
		{"1 approve r", record, 2, `line 1: "approve" is not a verdict; use accept, reject, or defer; nothing recorded`},
		{"1 accept a\n2 accept", record, 2, `line 2: finding 2 needs a reason after accept; nothing recorded`},
		{"1 accept " + strings.Repeat("a", 241), record, 2, `line 1: the reason is 241 bytes; shorten it to 240; nothing recorded`},
		{"1 accept a\n\n1 reject b", record, 2, `line 3: finding 1 is already judged on line 1; keep one line per finding; nothing recorded`},
		{"1 accept a\n4 reject b", record, 1, `line 2: review ` + string(costThreeReview) + ` (completed) has findings 1-3, not 4; nothing recorded`},
		{"1 accept a", []string{"finding", "record", string(costCleanReview)}, 1, `line 1: review ` + string(costCleanReview) + ` (completed) has no findings; nothing recorded`},
		{"1 accept a", []string{"finding", "record", string(costMissingReview)}, 1, `no review with id "` + string(costMissingReview) + `"; nothing recorded`},
		{"1 accept a", []string{"finding", "record", string(costBundle)}, 2, `"` + string(costBundle) + `" is a review bundle; name one of its member reviews (rp_...); nothing recorded`},
		{"1 accept a", []string{"finding", "record", "rp_nope"}, 2, `invalid review id "rp_nope"; expected rp_...; nothing recorded`},
		{"1 accept a", append(record, "--format", "yaml"), 2, `unknown output format "yaml"`},
		{"", []string{"finding", "list", "--format", "yaml"}, 2, `unknown output format "yaml"`},
	} {
		if got, want := findingCLI(test.stdin, test.arguments...), (cliRun{test.exit, "", "review-party: " + test.stderr + "\n"}); got != want {
			t.Errorf("%q %v = %+v, want %+v", test.stdin, test.arguments, got, want)
		}
	}
	requireFindingCLI(t, `{"verdicts":[]}`+"\n", "", "finding", "list", "--format", "json")
}

func TestFindingListShowsTheCurrentVerdictOnEachJudgedFinding(t *testing.T) {
	newFindingLedger(t)
	requireFindingCLI(t, "recorded 2\n", "2 accept real\n1 reject noise\n", "finding", "record", string(costThreeReview))
	requireFindingCLI(t, "recorded 1 (1 changed)\n", "2 defer later\n", "finding", "record", string(costThreeReview))
	requireFindingCLI(t, "recorded 1\n", "1 accept yes\n", "finding", "record", string(costOneReview))
	three := string(costThreeReview) + " · bugs · finding "
	requireFindingCLI(t, three+"1 · internal/file01.go:1 · rejected: noise\n"+three+"2 · internal/file02.go:2 · deferred: later\n", "", "finding", "list", string(costThreeReview))
	requireFindingCLI(t, string(costOneReview)+" · security · finding 1 · internal/file01.go:1 · accepted: yes\n", "", "finding", "list", "--profile", "security")
}

func TestInspectTreatsAVerdictOnChangedFindingTextAsUnjudged(t *testing.T) {
	state := newFindingLedger(t)
	requireFindingCLI(t, "recorded 3\n", "1 accept a\n2 accept b\n3 accept c\n", "finding", "record", string(costThreeReview))
	ledger, err := store.NewLedgerRecordStore(state)
	if err != nil {
		t.Fatal(err)
	}
	record := costRecord(costThreeReview, "bugs", 3)
	record.Result.Findings[1].Failure = "A different failure."
	seedReviews(record)(t, ledger)
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	verdicts, feedback := inspectFindingVerdicts(t, costThreeReview)
	if want := []*verdictView{{model.VerdictAccepted, "a"}, nil, {model.VerdictAccepted, "c"}}; !reflect.DeepEqual(verdicts, want) {
		t.Fatalf("verdicts = %v, want %v with finding 2 unjudged", verdicts, want)
	}
	if feedback == "" {
		t.Fatal("feedback is empty, want the hint back while finding 2 is unjudged")
	}
	three := string(costThreeReview) + " · bugs · finding "
	requireFindingCLI(t, three+"1 · internal/file01.go:1 · accepted: a\n"+three+"2 · internal/file02.go:2 · accepted: b · stale\n"+three+"3 · internal/file03.go:3 · accepted: c\n", "", "finding", "list", string(costThreeReview))
}

type unannotatableConductor struct{ *engine.Conductor }

func (unannotatableConductor) FindingVerdicts(context.Context, store.VerdictQuery) ([]model.FindingVerdict, error) {
	return nil, errors.New("ledger is busy")
}

func TestWaitShowsVerdictsAndWarnsWhenItCannotLoadThem(t *testing.T) {
	newFindingLedger(t)
	requireFindingCLI(t, "recorded 1\n", "1 accept real\n", "finding", "record", string(costThreeReview))
	if stdout := succeedCLI(t, "", "wait", string(costThreeReview)); !strings.Contains(stdout, "     accepted: real\n") {
		t.Fatalf("wait stdout =\n%s\nwant the recorded verdict", stdout)
	}

	conductor, err := engine.New(engine.Config{UserConfigurationPath: defaultUserConfigurationPath()})
	if err != nil {
		t.Fatal(err)
	}
	record, err := conductor.Inspect(context.Background(), costThreeReview)
	if err != nil {
		t.Fatal(err)
	}
	var want, got, warning bytes.Buffer
	if err := printReport(&want, recordReport(record, false), reportOptions{format: "human"}); err != nil {
		t.Fatal(err)
	}
	exit := executeWait(context.Background(), unannotatableConductor{conductor}, waitOptions{id: string(costThreeReview), format: "human"}, commandIO{output: &got, errors: &warning})
	if exit != 0 || warning.String() != "review-party: warning: load finding verdicts: ledger is busy; showing the result without misses or verdicts\n" {
		t.Fatalf("exit = %d, stderr = %q, want 0 and the warning", exit, warning.String())
	}
	if got.String() != want.String() {
		t.Fatalf("stdout =\n%s\nwant the unannotated result =\n%s", got.String(), want.String())
	}
}
