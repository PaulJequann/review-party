package reviewparty

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const cleanReview = `BEGIN_REVIEW
status: clean
summary: No actionable findings.
END_REVIEW`

const findingsReview = `BEGIN_REVIEW
status: findings

1. HIGH | correctness | review.go:3
Failure: The changed state is not handled.
Evidence: The Subject changes the state without updating its caller.
Fix: Update the caller with the state change.
Test: Exercise the caller with the changed state.
END_REVIEW`

type scriptedExecutor struct {
	availability availability
	execute      func(context.Context, attemptSpec) attemptExecution

	mu       sync.Mutex
	checks   int
	attempts []attemptSpec
}

func (executor *scriptedExecutor) Check(context.Context, reviewerCandidate) availability {
	executor.mu.Lock()
	executor.checks++
	executor.mu.Unlock()
	return executor.availability
}

func (executor *scriptedExecutor) Execute(ctx context.Context, spec attemptSpec) attemptExecution {
	executor.mu.Lock()
	executor.attempts = append(executor.attempts, spec)
	executor.mu.Unlock()
	return executor.execute(ctx, spec)
}

func (executor *scriptedExecutor) attemptCount() int {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return len(executor.attempts)
}

func (executor *scriptedExecutor) checkCount() int {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return executor.checks
}

func (executor *scriptedExecutor) lastAttempt() attemptSpec {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return executor.attempts[len(executor.attempts)-1]
}

func TestReviewFreezesWorkingChangesBeforeExecution(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"first change\"\n")

	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(_ context.Context, _ attemptSpec) attemptExecution {
			writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"later change\"\n")
			return attemptExecution{AssistantText: cleanReview, Outcome: AttemptCompleted}
		},
	}
	conductor := testConductor(t, executor, time.Second)
	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(record.Subject.Patch, "first change") {
		t.Fatalf("frozen subject does not contain original change:\n%s", record.Subject.Patch)
	}
	if strings.Contains(record.Subject.Patch, "later change") {
		t.Fatalf("frozen subject changed during execution:\n%s", record.Subject.Patch)
	}
}

func TestReviewCompletesOnlyWithValidCleanResult(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleCompleted {
		t.Fatalf("lifecycle = %q, want %q", record.Lifecycle, LifecycleCompleted)
	}
	if record.Result == nil || record.Result.Status != ResultClean {
		t.Fatalf("result = %#v, want clean", record.Result)
	}
	if record.AttemptCount() != 1 || record.Passes[0].Attempts[0].Outcome != AttemptCompleted {
		t.Fatalf("attempts = %#v, want one completed attempt", record.Passes[0].Attempts)
	}
}

func TestReviewPreservesValidFindings(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testConductor(t, successfulExecutor(findingsReview), time.Second)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleCompleted {
		t.Fatalf("lifecycle = %q, want %q", record.Lifecycle, LifecycleCompleted)
	}
	if record.Result == nil {
		t.Fatal("result is nil")
	}
	if record.Result.Status != ResultFindings {
		t.Fatalf("status = %q", record.Result.Status)
	}
	if record.Result.FindingCount != 1 {
		t.Fatalf("result = %#v, want one finding", record.Result)
	}
}

func TestMalformedOutputIsIncompleteNeverClean(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor("I found nothing concerning.")
	conductor := testConductor(t, executor, time.Second)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleIncomplete || record.Result != nil {
		t.Fatalf("record = %#v, want incomplete without result", record)
	}
	if record.Passes[0].Attempts[0].Outcome != AttemptInvalidResult {
		t.Fatalf("outcome = %q, want %q", record.Passes[0].Attempts[0].Outcome, AttemptInvalidResult)
	}
	assertTermination(t, record, TerminationResultValidationFailure, PhaseResultValidation)
}

func TestFailedExecutionCannotBeCompletedByValidPayload(t *testing.T) {
	repository := changedTestRepository(t)
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(context.Context, attemptSpec) attemptExecution {
			return attemptExecution{
				AssistantText: cleanReview,
				Outcome:       AttemptTransientFailure,
				Diagnostic:    "reviewer connection closed",
			}
		},
	}
	conductor := testConductor(t, executor, time.Second)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleIncomplete || record.Result != nil {
		t.Fatalf("record = %#v, want incomplete without a result", record)
	}
	if record.Passes[0].Attempts[0].Outcome != AttemptTransientFailure {
		t.Fatalf("outcome = %q, want %q", record.Passes[0].Attempts[0].Outcome, AttemptTransientFailure)
	}
	assertTermination(t, record, TerminationTransportFailure, PhaseReviewerExecution)
}

func TestUnavailableReviewerLaunchesNoAttempt(t *testing.T) {
	repository := changedTestRepository(t)
	executor := &scriptedExecutor{
		availability: availability{Diagnostic: "grok is not installed"},
		execute: func(context.Context, attemptSpec) attemptExecution {
			t.Fatal("unavailable reviewer was executed")
			return attemptExecution{}
		},
	}
	conductor := testConductor(t, executor, time.Second)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleIncomplete {
		t.Fatalf("lifecycle = %q", record.Lifecycle)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("launches = %d", executor.attemptCount())
	}
	if record.AttemptCount() != 0 {
		t.Fatalf("record = %#v, launches = %d; want incomplete with no attempts", record, executor.attemptCount())
	}
	assertTermination(t, record, TerminationReviewerUnavailable, PhaseAvailabilityCheck)
}

func TestExplicitReviewerRoutesToMatchingAdapter(t *testing.T) {
	repository := changedTestRepository(t)
	grok := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(context.Context, attemptSpec) attemptExecution {
			t.Fatal("grok executed for an opencode selection")
			return attemptExecution{}
		},
	}
	opencode := successfulExecutor(cleanReview)
	conductor := testConductorWithExecutors(t, map[string]attemptExecutor{
		"grok":     grok,
		"opencode": opencode,
	}, time.Second)
	selection := testSelection(repository)
	selection.Reviewer = "opencode"
	selection.Model = "meta/muse-spark-1.2-contributor"

	record, err := conductor.Review(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if grok.attemptCount() != 0 || opencode.attemptCount() != 1 {
		t.Fatalf("grok attempts = %d, opencode attempts = %d", grok.attemptCount(), opencode.attemptCount())
	}
	provenance := record.Passes[0].Attempts[0].Provenance
	if provenance.ReviewerID != "opencode" || provenance.Model != "meta/muse-spark-1.2-contributor" {
		t.Fatalf("provenance = %#v, want selected opencode reviewer", provenance)
	}
}

func TestReviewCarriesExplicitEffortToReviewer(t *testing.T) {
	repository := changedTestRepository(t)
	opencode := successfulExecutor(cleanReview)
	conductor := testConductorWithExecutors(t, map[string]attemptExecutor{"opencode": opencode}, time.Second)
	selection := testSelection(repository)
	selection.Reviewer = "opencode"
	selection.Model = "meta/muse-spark-1.2-contributor"
	selection.Effort = "high"

	record, err := conductor.Review(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if got := opencode.lastAttempt().Candidate.Effort; got != "high" {
		t.Fatalf("executed effort = %q, want high", got)
	}
	if got := record.ProfileRevision.Reviewer.Effort; got != "high" {
		t.Fatalf("recorded effort = %q, want high", got)
	}
}

func TestUnavailableReviewerDoesNotFallBack(t *testing.T) {
	repository := changedTestRepository(t)
	grok := &scriptedExecutor{
		availability: availability{Diagnostic: "grok login required"},
		execute: func(context.Context, attemptSpec) attemptExecution {
			t.Fatal("unavailable grok reviewer executed")
			return attemptExecution{}
		},
	}
	opencode := successfulExecutor(cleanReview)
	conductor := testConductorWithExecutors(t, map[string]attemptExecutor{
		"grok":     grok,
		"opencode": opencode,
	}, time.Second)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleIncomplete {
		t.Fatalf("lifecycle = %q", record.Lifecycle)
	}
	if record.AttemptCount() != 0 {
		t.Fatalf("attempt count = %d", record.AttemptCount())
	}
	if opencode.attemptCount() != 0 {
		t.Fatalf("record = %#v, opencode attempts = %d; want no fallback", record, opencode.attemptCount())
	}
}

func TestAttemptDeadlineProducesInspectableIncompleteRecord(t *testing.T) {
	repository := changedTestRepository(t)
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(ctx context.Context, _ attemptSpec) attemptExecution {
			<-ctx.Done()
			return attemptExecution{
				Outcome:         AttemptTransientFailure,
				Diagnostic:      ctx.Err().Error(),
				FailureCategory: TerminationDeadlineExceeded,
				FailurePhase:    PhaseReviewerExecution,
			}
		},
	}
	conductor := testConductor(t, executor, 20*time.Millisecond)

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleIncomplete {
		t.Fatalf("lifecycle = %q, want incomplete", record.Lifecycle)
	}
	stored, err := conductor.Inspect(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(record, stored) {
		t.Fatalf("stored record differs\nrecord: %#v\nstored: %#v", record, stored)
	}
	assertTermination(t, record, TerminationDeadlineExceeded, PhaseReviewerExecution)
	if !strings.Contains(record.Termination.Message, context.DeadlineExceeded.Error()) {
		t.Fatalf("termination message = %q", record.Termination.Message)
	}
}

func TestReviewRecordsCoherentOperationalTimingAndBuildProvenance(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testConductor(t, successfulExecutor(cleanReview), time.Second)
	instant := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	conductor.now = func() time.Time {
		instant = instant.Add(10 * time.Millisecond)
		return instant
	}
	modified := false
	conductor.buildProvenance = func() RuntimeProvenance {
		return RuntimeProvenance{Version: "0.4.0", VCSRevision: "abc123", VCSModified: &modified}
	}

	record, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	assertOperationalSchemaAndRuntime(t, record)
	assertCoherentTimings(t, record)
}

func assertOperationalSchemaAndRuntime(t *testing.T, record ReviewRecord) {
	t.Helper()
	if record.SchemaVersion != currentReviewRecordSchemaVersion {
		t.Fatalf("schema version = %d", record.SchemaVersion)
	}
	if record.Runtime.Version != "0.4.0" {
		t.Fatalf("runtime version = %q", record.Runtime.Version)
	}
	if record.Runtime.VCSRevision != "abc123" {
		t.Fatalf("runtime revision = %q", record.Runtime.VCSRevision)
	}
	if record.Runtime.VCSModified == nil {
		t.Fatalf("runtime modified = %#v", record.Runtime.VCSModified)
	}
	if *record.Runtime.VCSModified {
		t.Fatal("runtime was unexpectedly modified")
	}
}

func assertCoherentTimings(t *testing.T, record ReviewRecord) {
	t.Helper()
	phaseTotal := record.Timings.SubjectResolutionMS + record.Timings.ProfileCompilationMS +
		record.Timings.AvailabilityCheckMS + record.Timings.AttemptExecutionMS +
		record.Timings.ResultValidationMS
	if phaseTotal <= 0 {
		t.Fatalf("timings = %#v, phase total = %d", record.Timings, phaseTotal)
	}
	if record.Timings.TotalMS < phaseTotal {
		t.Fatalf("timings = %#v, phase total = %d", record.Timings, phaseTotal)
	}
}

func TestElapsedMillisecondsNeverReportsNegativeDuration(t *testing.T) {
	later := time.Date(2026, time.August, 9, 12, 0, 1, 0, time.UTC)
	if got := elapsedMilliseconds(later, later.Add(-time.Second)); got != 0 {
		t.Fatalf("elapsed milliseconds = %d, want 0", got)
	}
}

func TestWorkingChangesIncludesUntrackedFilesWithoutHead(t *testing.T) {
	repository := t.TempDir()
	runTestCommand(t, repository, "git", "init", "--quiet")
	writeTestFile(t, filepath.Join(repository, "new.go"), "package demo\n")

	subject, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(subject.ChangedPaths, []string{"new.go"}) {
		t.Fatalf("changed paths = %#v, want new.go", subject.ChangedPaths)
	}
	if !strings.Contains(subject.Patch, "package demo") {
		t.Fatalf("patch does not include untracked file:\n%s", subject.Patch)
	}
}

func successfulExecutor(output string) *scriptedExecutor {
	return &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(context.Context, attemptSpec) attemptExecution {
			return attemptExecution{AssistantText: output, Outcome: AttemptCompleted}
		},
	}
}

func testConductor(t *testing.T, executor attemptExecutor, deadline time.Duration) *Conductor {
	return testConductorWithExecutors(t, map[string]attemptExecutor{defaultReviewer: executor}, deadline)
}

func testConductorWithExecutors(t *testing.T, executors map[string]attemptExecutor, deadline time.Duration) *Conductor {
	t.Helper()
	store, err := newFileRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return newConductor(store, executors, deadline)
}

func testSelection(repository string) ReviewSelection {
	return ReviewSelection{Repository: repository, Subject: WorkingChanges(), Profile: "bugs"}
}

func changedTestRepository(t *testing.T) string {
	t.Helper()
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n")
	return repository
}

func testRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runTestCommand(t, repository, "git", "init", "--quiet")
	runTestCommand(t, repository, "git", "config", "user.email", "review-party@example.invalid")
	runTestCommand(t, repository, "git", "config", "user.name", "Review Party Test")
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"base\"\n")
	runTestCommand(t, repository, "git", "add", "review.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: establish base")
	return repository
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runTestCommand(t *testing.T, directory, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, arguments, err, output)
	}
}

func slicesContainPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func assertTermination(t *testing.T, record ReviewRecord, category TerminationCategory, phase ExecutionPhase) {
	t.Helper()
	if record.Termination == nil {
		t.Fatal("termination is nil")
	}
	if record.Termination.Category != category || record.Termination.Phase != phase {
		t.Fatalf("termination = %#v, want category %q phase %q", record.Termination, category, phase)
	}
}
