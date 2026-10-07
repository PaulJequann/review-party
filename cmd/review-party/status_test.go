package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

const (
	runningBundleID    = "rb_1725192000000_00000000000000b1"
	completedBundleID  = "rb_1725192000000_00000000000000b2"
	incompleteBundleID = "rb_1725192000000_00000000000000b3"
	stoppedBundleID    = "rb_1725192000000_00000000000000b4"
	unreadableBundleID = "rb_1725192000000_00000000000000b5"
	unreadableReviewID = "rp_1725192000000_00000000000000c7"
	pendingReviewID    = "rp_1725192000000_00000000000000a1"
	incompleteReviewID = "rp_1725192000000_00000000000000a2"
	completedReviewID  = "rp_1725192000000_00000000000000a3"
)

type statusLedger struct {
	t          *testing.T
	directory  string
	repository string
	root       string
	base       time.Time
}

func newStatusLedger(t *testing.T) statusLedger {
	t.Helper()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	repository := testGitRepository(t)
	runMainCommand(t, []string{"init", "--repo", repository})
	root, err := subject.ResolveRepositoryRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	fixture := statusLedger{t: t, directory: filepath.Join(stateHome, "review-party"), repository: repository, root: root, base: time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)}
	fixture.write(func(ledger *store.LedgerRecordStore) error {
		bundles := []struct {
			id          model.ReviewBundleID
			lifecycle   model.Lifecycle
			termination *model.BundleTermination
			members     []model.ReviewRecord
		}{
			{runningBundleID, model.LifecycleRunning, nil, []model.ReviewRecord{
				fixture.record("rp_1725192000000_00000000000000c1", model.LifecycleCompleted, 1),
				fixture.record("rp_1725192000000_00000000000000c2", model.LifecycleRunning, 1),
			}},
			{completedBundleID, model.LifecycleCompleted, nil, []model.ReviewRecord{fixture.record("rp_1725192000000_00000000000000c3", model.LifecycleCompleted, 2)}},
			{incompleteBundleID, model.LifecycleIncomplete, nil, []model.ReviewRecord{fixture.record("rp_1725192000000_00000000000000c4", model.LifecycleIncomplete, 3)}},
			{stoppedBundleID, model.LifecycleIncomplete, &model.BundleTermination{Category: model.TerminationCancelled, Message: "context canceled"}, []model.ReviewRecord{fixture.record("rp_1725192000000_00000000000000c5", model.LifecycleIncomplete, 4)}},
		}
		for index, bundle := range bundles {
			recorded := fixture.bundle(bundle.id, bundle.lifecycle, index, bundle.members)
			recorded.Termination = bundle.termination
			if err := ledger.CreateReviewBundle(recorded, bundle.members); err != nil {
				return err
			}
		}
		readable := fixture.record("rp_1725192000000_00000000000000c6", model.LifecycleCompleted, 4)
		unreadable := fixture.bundle(unreadableBundleID, model.LifecycleCompleted, 4, []model.ReviewRecord{readable, fixture.record(unreadableReviewID, model.LifecycleCompleted, 4)})
		if err := ledger.CreateReviewBundle(unreadable, []model.ReviewRecord{readable}); err != nil {
			return err
		}
		for _, record := range []model.ReviewRecord{
			fixture.record(pendingReviewID, model.LifecyclePending, 5),
			fixture.record(incompleteReviewID, model.LifecycleIncomplete, 6),
			fixture.record(completedReviewID, model.LifecycleCompleted, 7),
		} {
			if err := ledger.Save(record); err != nil {
				return err
			}
		}
		return nil
	})
	return fixture
}

func (fixture statusLedger) write(change func(*store.LedgerRecordStore) error) {
	fixture.t.Helper()
	ledger, err := store.NewLedgerRecordStore(fixture.directory)
	if err != nil {
		fixture.t.Fatal(err)
	}
	if err := change(ledger); err != nil {
		fixture.t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture statusLedger) record(id model.ReviewID, lifecycle model.Lifecycle, minute int) model.ReviewRecord {
	created := fixture.base.Add(time.Duration(minute) * time.Minute)
	record := model.ReviewRecord{
		SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: id, Lifecycle: lifecycle,
		Subject:         model.ReviewSubject{Kind: model.SubjectWorkingChanges, Repository: fixture.root, Identity: "subject", ChangedPaths: []string{"a.go"}},
		ProfileRevision: model.ProfileRevision{Name: "bugs", ReviewerID: "grok", Model: "grok-4.5", ExecutionDeadline: "8m0s"},
		ProfileSnapshot: model.ProfileSnapshot{Name: "bugs"},
		Passes:          []model.PassRecord{{Name: "review", Required: true, Attempts: []model.AttemptRecord{}}},
		CreatedAt:       created, UpdatedAt: created,
	}
	if lifecycle.Terminal() {
		record.Passes[0].Attempts = []model.AttemptRecord{{Number: 1, Outcome: model.AttemptCompleted, Provenance: model.ReviewerProvenance{ReviewerID: "grok", Model: "grok-4.5"}, StartedAt: created, CompletedAt: created}}
		record.Timings = &model.ReviewTimings{TotalMS: 65000}
	}
	switch lifecycle {
	case model.LifecyclePending, model.LifecycleRunning:
	case model.LifecycleCompleted:
		record.Result = &model.ReviewResult{Status: model.ResultFindings, Summary: "two findings", Findings: []model.Finding{{Ordinal: 1}, {Ordinal: 2}}}
	case model.LifecycleIncomplete:
		record.Termination = &model.ReviewTermination{Category: model.TerminationDeadlineExceeded, Phase: model.PhaseReviewerExecution, Message: "attempt exceeded 8m0s"}
	}
	return record
}

func (fixture statusLedger) bundle(id model.ReviewBundleID, lifecycle model.Lifecycle, minute int, records []model.ReviewRecord) model.ReviewBundle {
	created := fixture.base.Add(time.Duration(minute) * time.Minute)
	bundle := model.ReviewBundle{
		ID: id, Revision: "revision", Repository: fixture.root, SubjectKind: model.SubjectWorkingChanges, SubjectIdentity: "subject",
		Lifecycle: lifecycle, Members: []model.BundleMember{},
		Warnings: []model.BundleWarning{}, Deduplicated: []model.SkippedDuplicate{},
		ConcurrencyLimit: 1, CreatedAt: created, UpdatedAt: created,
	}
	for _, record := range records {
		member := model.BundleMember{Scope: "global", Profile: record.ProfileRevision.Name, ReviewID: record.ID, Lifecycle: record.Lifecycle}
		if record.Result != nil {
			member.Status, member.FindingCount = string(record.Result.Status), record.Result.FindingCount()
		}
		bundle.Members = append(bundle.Members, member)
	}
	return bundle
}

func runCLI(arguments ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), arguments, &stdout, &stderr)
	return exit, stdout.String(), stderr.String()
}

func TestStatusReportsARunningBundleWithoutWaiting(t *testing.T) {
	newStatusLedger(t)
	exit, stdout, stderr := runCLI("status", runningBundleID)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 4 ||
		lines[0] != "bundle "+runningBundleID+" · running · 1/2 review(s) finished" ||
		!strings.HasPrefix(lines[1], "  global:bugs") || !strings.HasSuffix(lines[1], "completed rp_1725192000000_00000000000000c1 · 2 finding(s) · 1m5s") ||
		!strings.Contains(lines[2], "running rp_1725192000000_00000000000000c2 · attempt 1 · updated ") || !strings.HasSuffix(lines[2], " ago") ||
		lines[3] != "wait: review-party wait "+runningBundleID {
		t.Fatalf("status output =\n%s", stdout)
	}
}

func TestStatusReportsFinishedRunsAndPointsAtInspect(t *testing.T) {
	newStatusLedger(t)
	exit, stdout, stderr := runCLI("status", stoppedBundleID)
	if exit != 0 || !strings.HasPrefix(stdout, "bundle "+stoppedBundleID+" · incomplete · 1/1 review(s) finished\n") ||
		!strings.Contains(stdout, "incomplete rp_1725192000000_00000000000000c5 · deadline_exceeded · 1m5s\n") ||
		!strings.Contains(stdout, "stopped: cancelled: context canceled\n") || !strings.HasSuffix(stdout, "inspect: review-party inspect "+stoppedBundleID+"\n") {
		t.Fatalf("stopped bundle exit = %d, stdout =\n%s\nstderr = %q", exit, stdout, stderr)
	}
	exit, stdout, stderr = runCLI("status", incompleteReviewID, "--format", "json")
	var status model.ReviewStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil || exit != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q, decode error = %v", exit, stdout, stderr, err)
	}
	if status.Kind != model.ReviewStatusReview || status.Lifecycle != model.LifecycleIncomplete || len(status.Reviews) != 1 ||
		status.Reviews[0].Termination == nil || status.Reviews[0].Termination.Category != model.TerminationDeadlineExceeded || status.Reviews[0].DurationMS != 65000 {
		t.Fatalf("status = %#v, want the incomplete Review with its termination", status)
	}
}

func TestStatusRejectsUnknownAndUnsupportedIDs(t *testing.T) {
	newStatusLedger(t)
	unknown := "rp_1725192000000_0123456789abcdef"
	if exit, _, stderr := runCLI("status", unknown); exit != 1 || stderr != "review-party: no review with id \""+unknown+"\"\n" {
		t.Fatalf("unknown id exit = %d, stderr = %q", exit, stderr)
	}
	if exit, _, stderr := runCLI("status", "ev_1725192000000_0123456789abcdef"); exit != usageExitCode || !strings.Contains(stderr, "rb_") {
		t.Fatalf("unsupported id exit = %d, stderr = %q", exit, stderr)
	}
}

func TestStatusWithoutIDListsInFlightRunsNewestFirst(t *testing.T) {
	fixture := newStatusLedger(t)
	exit, stdout, stderr := runCLI("status", "--repo", fixture.repository, "--format", "json")
	var report struct {
		Repository string               `json:"repository"`
		InFlight   []model.ReviewStatus `json:"in_flight"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || exit != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q, decode error = %v", exit, stdout, stderr, err)
	}
	if report.Repository != fixture.root || len(report.InFlight) != 2 || report.InFlight[0].ID != pendingReviewID || report.InFlight[1].ID != runningBundleID {
		t.Fatalf("report = %#v, want the pending Review then the running bundle for %s", report, fixture.root)
	}

	empty := testGitRepository(t)
	emptyRoot, err := subject.ResolveRepositoryRoot(empty)
	if err != nil {
		t.Fatal(err)
	}
	if exit, stdout, _ := runCLI("status", "--repo", empty); exit != 0 || stdout != "no reviews in flight for "+emptyRoot+"\n" {
		t.Fatalf("empty human exit = %d, stdout = %q", exit, stdout)
	}
	if exit, stdout, _ := runCLI("status", "--repo", empty, "--format", "json"); exit != 0 || stdout != `{"repository":"`+emptyRoot+`","in_flight":[]}`+"\n" {
		t.Fatalf("empty json exit = %d, stdout = %q", exit, stdout)
	}
}

func TestStatusPrintsAnUnreadableMemberAndFails(t *testing.T) {
	newStatusLedger(t)
	exit, stdout, stderr := runCLI("status", unreadableBundleID)
	readError := `no review with id "` + unreadableReviewID + `"`
	if exit != 1 || !strings.HasPrefix(stdout, "bundle "+unreadableBundleID+" · completed · 1/2 review(s) finished\n") ||
		!strings.Contains(stdout, "unreadable "+unreadableReviewID+" · "+readError+"\n") ||
		stderr != "review-party: read bundle member global:bugs review "+unreadableReviewID+": "+readError+"\n" {
		t.Fatalf("exit = %d, stdout =\n%s\nstderr = %q", exit, stdout, stderr)
	}
	exit, stdout, _ = runCLI("status", unreadableBundleID, "--format", "json")
	var status model.ReviewStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil || exit != 1 || len(status.Reviews) != 2 {
		t.Fatalf("exit = %d, stdout = %q, decode error = %v", exit, stdout, err)
	}
	if member := status.Reviews[1]; member.ReviewID != unreadableReviewID || member.Lifecycle != "unreadable" || member.ReadError != readError {
		t.Fatalf("unreadable member = %#v", member)
	}
}
