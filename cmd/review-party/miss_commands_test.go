package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

const (
	missReviewBugs     model.ReviewID       = "rp_1723200000000_0123456789abcdef"
	missReviewSecurity model.ReviewID       = "rp_1723200000001_0123456789abcdef"
	missReviewOther    model.ReviewID       = "rp_1723200000002_0123456789abcdef"
	missReviewRunning  model.ReviewID       = "rp_1723200000003_0123456789abcdef"
	missBundle         model.ReviewBundleID = "rb_1723200000000_0123456789abcdef"
	missMixedBundle    model.ReviewBundleID = "rb_1723200000001_0123456789abcdef"
	missUnrunBundle    model.ReviewBundleID = "rb_1723200000002_0123456789abcdef"
)

type missLedger struct {
	configuration string
	repository    string
	other         string
}

func newMissLedger(t *testing.T) missLedger {
	t.Helper()
	state := t.TempDir()
	fixture := missLedger{
		configuration: filepath.Join(t.TempDir(), "config.json"),
		repository:    resolvedTestRepository(t),
		other:         resolvedTestRepository(t),
	}
	runMainCommand(t, []string{"init", "--repo", fixture.repository, "--state-dir", state, "--config", fixture.configuration})
	ledger, err := store.NewLedgerRecordStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, record := range []model.ReviewRecord{
		missReview(missReviewBugs, "bugs", fixture.repository, model.LifecycleCompleted),
		missReview(missReviewSecurity, "security", fixture.repository, model.LifecycleCompleted),
		missReview(missReviewOther, "bugs", fixture.other, model.LifecycleCompleted),
		missReview(missReviewRunning, "style", fixture.repository, model.LifecycleIncomplete),
	} {
		if err := ledger.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	for _, bundle := range []model.ReviewBundle{
		missReviewBundle(missBundle, missReviewBugs, missReviewSecurity),
		missReviewBundle(missMixedBundle, missReviewBugs, missReviewRunning),
		missReviewBundle(missUnrunBundle, missReviewBugs, ""),
	} {
		if err := ledger.CreateReviewBundle(bundle); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func resolvedTestRepository(t *testing.T) string {
	t.Helper()
	root, err := subject.ResolveRepositoryRoot(testGitRepository(t))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func missReview(id model.ReviewID, profile, repository string, lifecycle model.Lifecycle) model.ReviewRecord {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return model.ReviewRecord{
		SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: id, Lifecycle: lifecycle, CreatedAt: now, UpdatedAt: now,
		Subject:         model.ReviewSubject{Kind: model.SubjectWorkingChanges, Repository: repository, Identity: "0123456789abcdef0123"},
		ProfileRevision: model.ProfileRevision{Name: profile},
	}
}

func missReviewBundle(id model.ReviewBundleID, reviews ...model.ReviewID) model.ReviewBundle {
	names := map[model.ReviewID]string{missReviewBugs: "bugs", missReviewSecurity: "security", missReviewRunning: "style", "": "perf"}
	members := make([]model.BundleMember, 0, len(reviews))
	for _, review := range reviews {
		members = append(members, model.BundleMember{Scope: "global", Profile: names[review], Lifecycle: model.LifecycleCompleted, ReviewID: review})
	}
	return model.ReviewBundle{
		ID: id, Lifecycle: model.LifecycleCompleted, Members: members,
		Warnings: []model.BundleWarning{}, Deduplicated: []model.SkippedDuplicate{},
	}
}

func (fixture missLedger) run(arguments ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), append(arguments, "--config", fixture.configuration), &stdout, &stderr)
	return exit, stdout.String(), stderr.String()
}

func (fixture missLedger) add(t *testing.T, arguments ...string) []model.Miss {
	t.Helper()
	base := []string{"miss", "add", "--path", "internal/a.go", "--source", "codex-pr", "--description", "nil map write", "--recorded-by", "pj", "--format", "json"}
	return decodeMissOutput[[]model.Miss](t, fixture, append(base, arguments...)...)
}

func (fixture missLedger) list(t *testing.T, arguments ...string) []model.Miss {
	t.Helper()
	return decodeMissOutput[[]model.Miss](t, fixture, append([]string{"miss", "list", "--format", "json"}, arguments...)...)
}

func decodeMissOutput[T any](t *testing.T, fixture missLedger, arguments ...string) T {
	t.Helper()
	exit, stdout, stderr := fixture.run(arguments...)
	if exit != 0 {
		t.Fatalf("%v exit = %d, stderr = %q", arguments, exit, stderr)
	}
	var decoded T
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	return decoded
}

func missReviewIDs(misses []model.Miss) []model.ReviewID {
	ids := make([]model.ReviewID, 0, len(misses))
	for _, miss := range misses {
		ids = append(ids, miss.ReviewID)
	}
	return ids
}

func requireMissReviews(t *testing.T, misses []model.Miss, want ...model.ReviewID) {
	t.Helper()
	if got := missReviewIDs(misses); !slices.Equal(got, want) {
		t.Fatalf("miss reviews = %v, want %v", got, want)
	}
}

func requireMissOutput(t *testing.T, fixture missLedger, want string, arguments ...string) {
	t.Helper()
	exit, stdout, stderr := fixture.run(arguments...)
	if exit != 0 || !strings.HasSuffix(stdout, want) {
		t.Fatalf("%v exit = %d, stdout = %q, stderr = %q, want suffix %q", arguments, exit, stdout, stderr, want)
	}
}

func TestMissAddOnCompletedReviewIsListed(t *testing.T) {
	fixture := newMissLedger(t)
	added := fixture.add(t, "--review", string(missReviewBugs), "--line", "12")
	listed := fixture.list(t)
	requireMissReviews(t, listed, missReviewBugs)
	want := model.Miss{
		ID: added[0].ID, ReviewID: missReviewBugs, Repository: fixture.repository,
		SubjectKind: model.SubjectWorkingChanges, SubjectIdentity: "0123456789abcdef0123", Profile: "bugs",
		Location: model.MissLocation{Path: "internal/a.go", Line: 12}, Source: model.MissSourceCodexPR,
		Description: "nil map write", RecordedBy: "pj", RecordedAt: added[0].RecordedAt,
	}
	if listed[0] != want {
		t.Fatalf("listed miss = %#v, want %#v", listed[0], want)
	}
	line := strings.Join([]string{
		string(want.ID), string(missReviewBugs), "bugs", "internal/a.go:12", "codex-pr", "nil map write",
		want.RecordedAt.UTC().Format(time.RFC3339),
	}, " · ")
	requireMissOutput(t, fixture, line+"\n", "miss", "list")
}

func TestMissAddOnUnknownTargetFails(t *testing.T) {
	fixture := newMissLedger(t)
	for target, message := range map[string]string{
		"rp_1723200000009_0123456789abcdef": `no review with id "rp_1723200000009_0123456789abcdef"`,
		"rb_1723200000009_0123456789abcdef": `no review bundle with id "rb_1723200000009_0123456789abcdef"`,
	} {
		exit, _, stderr := fixture.run("miss", "add", "--review", target, "--path", "a.go", "--source", "human", "--description", "d", "--recorded-by", "pj")
		if exit != 1 || !strings.Contains(stderr, message) {
			t.Fatalf("add on %s exit = %d, stderr = %q, want %q", target, exit, stderr, message)
		}
	}
	requireMissReviews(t, fixture.list(t))
}

func TestMissAddOnBundleAttachesToMembers(t *testing.T) {
	fixture := newMissLedger(t)
	requireMissReviews(t, fixture.add(t, "--review", string(missBundle)), missReviewBugs, missReviewSecurity)
	narrowed := fixture.add(t, "--review", string(missBundle), "--profile", "security")
	requireMissReviews(t, narrowed, missReviewSecurity)
	requireMissReviews(t, fixture.list(t), missReviewBugs, missReviewSecurity, missReviewSecurity)
}

func TestMissAddOnUnusableTargetWritesNothing(t *testing.T) {
	fixture := newMissLedger(t)
	for target, message := range map[[2]string]string{
		{string(missReviewRunning), ""}: "is incomplete; misses attach only to completed reviews",
		{string(missMixedBundle), ""}:   "cannot take a miss: style (" + string(missReviewRunning) + " incomplete)",
		{string(missUnrunBundle), ""}:   "cannot take a miss: perf (no review)",
		{string(missBundle), "perf"}:    `no member with profile "perf"; members: bugs, security`,
	} {
		exit, _, stderr := fixture.run("miss", "add", "--review", target[0], "--profile", target[1], "--path", "a.go", "--source", "human", "--description", "d", "--recorded-by", "pj")
		if exit != 1 || !strings.Contains(stderr, message) {
			t.Fatalf("add on %v exit = %d, stderr = %q, want %q", target, exit, stderr, message)
		}
	}
	requireMissReviews(t, fixture.list(t))
	requireMissReviews(t, fixture.add(t, "--review", string(missMixedBundle), "--profile", "bugs"), missReviewBugs)
}

func TestMissListFiltersByRepositoryAndProfile(t *testing.T) {
	fixture := newMissLedger(t)
	fixture.add(t, "--review", string(missReviewBugs))
	fixture.add(t, "--review", string(missReviewSecurity))
	fixture.add(t, "--review", string(missReviewOther))
	nested := filepath.Join(fixture.other, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	requireMissReviews(t, fixture.list(t, "--repo", nested), missReviewOther)
	requireMissReviews(t, fixture.list(t, "--repo", fixture.repository), missReviewBugs, missReviewSecurity)
	requireMissReviews(t, fixture.list(t, "--repo", fixture.repository, "--profile", "bugs"), missReviewBugs)
	requireMissReviews(t, fixture.list(t, "--profile", "bugs"), missReviewBugs, missReviewOther)
}

func TestMissCommandsRejectUsageErrors(t *testing.T) {
	fixture := newMissLedger(t)
	add := []string{"miss", "add", "--review", string(missReviewBugs), "--path", "a.go", "--description", "d", "--recorded-by", "pj"}
	for _, test := range []struct {
		arguments []string
		message   string
	}{
		{append(add, "--source", "bot"), "expected codex-pr, human, incident, or other"},
		{append(add, "--source", "human", "--line", "0"), "--line must be at least 1"},
		{append(add, "--source", "human", "--profile", "bugs"), "--profile narrows a review bundle"},
		{append(add[:2:2], "--review", "rx_1723200000000_0123456789abcdef", "--path", "a.go", "--source", "human", "--description", "d"), "invalid miss target"},
		{append(add[:4:4], "--source", "human", "--description", "d"), `"path" not set`},
		{[]string{"miss", "remove", "rp_1723200000000_0123456789abcdef", "--reason", "r"}, "invalid miss id"},
	} {
		exit, _, stderr := fixture.run(test.arguments...)
		if exit != usageExitCode || !strings.Contains(stderr, test.message) {
			t.Errorf("%v exit = %d, stderr = %q, want %q", test.arguments, exit, stderr, test.message)
		}
	}
	requireMissReviews(t, fixture.list(t))
}

func TestMissRemoveKeepsTombstone(t *testing.T) {
	fixture := newMissLedger(t)
	id := string(fixture.add(t, "--review", string(missReviewBugs))[0].ID)
	for _, status := range []string{"removed", "already removed"} {
		requireMissOutput(t, fixture, id+" "+status+"\n", "miss", "remove", id, "--reason", "not a bug", "--removed-by", "reviewer")
	}
	requireMissReviews(t, fixture.list(t))
	removed := fixture.list(t, "--include-removed")
	requireMissReviews(t, removed, missReviewBugs)
	if removed[0].Removal == nil {
		t.Fatal("removed miss has no tombstone")
	}
	want := model.MissRemoval{Reason: "not a bug", RemovedBy: "reviewer", RemovedAt: removed[0].Removal.RemovedAt}
	if *removed[0].Removal != want || want.RemovedAt.IsZero() {
		t.Fatalf("removal = %#v, want %#v", *removed[0].Removal, want)
	}
	requireMissOutput(t, fixture, " · removed: not a bug\n", "miss", "list", "--include-removed")
}

type inspectedMisses struct {
	Reviews []struct {
		ID     model.ReviewID `json:"id"`
		Misses *[]model.Miss  `json:"misses"`
	} `json:"reviews"`
}

func (fixture missLedger) inspectMisses(t *testing.T, id string) map[model.ReviewID][]model.Miss {
	t.Helper()
	report := decodeMissOutput[inspectedMisses](t, fixture, "inspect", id, "--format", "json")
	misses := make(map[model.ReviewID][]model.Miss, len(report.Reviews))
	for _, entry := range report.Reviews {
		if entry.Misses == nil {
			t.Fatalf("inspect %s review %s has no misses array", id, entry.ID)
		}
		misses[entry.ID] = *entry.Misses
	}
	return misses
}

func TestInspectShowsActiveMisses(t *testing.T) {
	fixture := newMissLedger(t)
	kept := fixture.add(t, "--review", string(missReviewBugs), "--line", "12")[0]
	removed := string(fixture.add(t, "--review", string(missReviewBugs), "--description", "false alarm")[0].ID)
	requireMissOutput(t, fixture, removed+" removed\n", "miss", "remove", removed, "--reason", "not a bug")

	misses := fixture.inspectMisses(t, string(missReviewBugs))[missReviewBugs]
	if len(misses) != 1 || misses[0] != kept {
		t.Fatalf("inspected misses = %#v, want only %#v", misses, kept)
	}
	exit, stdout, stderr := fixture.run("inspect", string(missReviewBugs))
	if exit != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", exit, stderr)
	}
	want := "  miss: internal/a.go:12 · codex-pr · nil map write (" + string(kept.ID) + ", recorded by pj)\n"
	if !strings.Contains(stdout, want) {
		t.Fatalf("inspect stdout = %q, want line %q", stdout, want)
	}
	if strings.Contains(stdout, "false alarm") {
		t.Fatalf("inspect stdout = %q shows a removed miss", stdout)
	}
}

func TestInspectBundleShowsEachMembersMisses(t *testing.T) {
	fixture := newMissLedger(t)
	requireMissReviews(t, fixture.add(t, "--review", string(missBundle), "--profile", "security"), missReviewSecurity)

	misses := fixture.inspectMisses(t, string(missBundle))
	requireMissReviews(t, misses[missReviewSecurity], missReviewSecurity)
	requireMissReviews(t, misses[missReviewBugs])
}
