package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const codexInputTooLargeRejection = `Error: turn/start: turn/start failed: Input exceeds the maximum length of 1048576 characters. (code -32602), data: {"input_error_code":"input_too_large","max_chars":1048576,"actual_chars":1063438}
`

func TestCodexInputRejectionIsClassifiedAsInputTooLarge(t *testing.T) {
	stdout := codexEventStream(`{"type":"thread.started","thread_id":"t"}`)

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: stdout, Stderr: codexInputTooLargeRejection, WaitErr: errors.New("exit status 1")})

	if execution.FailureCategory != model.TerminationInputTooLarge {
		t.Fatalf("category = %q, want %q", execution.FailureCategory, model.TerminationInputTooLarge)
	}
	if execution.FailurePhase != model.PhaseReviewerExecution {
		t.Fatalf("phase = %q", execution.FailurePhase)
	}
	for _, want := range []string{"1063438", "1048576"} {
		if !strings.Contains(execution.Diagnostic, want) {
			t.Fatalf("diagnostic = %q, want the measured size and the limit (%s)", execution.Diagnostic, want)
		}
	}
}

func TestInputTooLargeIsNeverRetried(t *testing.T) {
	termination := &model.ReviewTermination{Category: model.TerminationInputTooLarge, Phase: model.PhaseInputPreflight}
	if retryableTermination(termination) {
		t.Fatal("input_too_large must not be retried")
	}
}

func TestOnlyCodexDeclaresAnInputLimit(t *testing.T) {
	limits := map[string]int{}
	for id, registration := range defaultReviewerCatalog().registrations {
		limits[id] = registration.inputCharacterLimit
	}
	if limits["codex"] != 1048576 {
		t.Fatalf("codex limit = %d, want 1048576", limits["codex"])
	}
	for _, id := range []string{"grok", "opencode", "copilot", "claude"} {
		if limits[id] != 0 {
			t.Fatalf("%s limit = %d, want none declared", id, limits[id])
		}
	}
}

type sizeHarness struct {
	conductor *Conductor
	ledger    *store.LedgerRecordStore
	executor  *scriptedExecutor
	warnings  *warningLog
}

type warningLog struct {
	mu    sync.Mutex
	lines []string
}

func (log *warningLog) add(line string) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.lines = append(log.lines, line)
}

func (log *warningLog) all() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.lines...)
}

func newSizeHarness(t *testing.T, limit int) sizeHarness {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	executor := successfulExecutor(cleanReview)
	catalog := catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor})
	registration := catalog.registrations[defaultReviewer]
	registration.inputCharacterLimit = limit
	catalog.registrations[defaultReviewer] = registration
	conductor, err := newConductorWithManager(ledger, catalog, newTestConfigurationManager(t), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	warnings := &warningLog{}
	conductor.runner.warn = warnings.add
	return sizeHarness{conductor: conductor, ledger: ledger, executor: executor, warnings: warnings}
}

func unicodeChangedRepository(t *testing.T) string {
	t.Helper()
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \""+strings.Repeat("é", 400)+"\"\n")
	return repository
}

func measuredPrompt(t *testing.T, repository string) int {
	t.Helper()
	harness := newSizeHarness(t, 0)
	if _, err := harness.conductor.Review(context.Background(), testSelection(repository)); err != nil {
		t.Fatal(err)
	}
	if harness.executor.attemptCount() != 1 {
		t.Fatalf("baseline launches = %d", harness.executor.attemptCount())
	}
	return utf8.RuneCountInString(harness.executor.attempts[0].Prompt)
}

func TestOversizedSubjectFailsBeforeAnyReviewerLaunches(t *testing.T) {
	repository := unicodeChangedRepository(t)
	characters := measuredPrompt(t, repository)
	harness := newSizeHarness(t, characters-1)

	record, err := harness.conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}

	if record.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("lifecycle = %q", record.Lifecycle)
	}
	assertTermination(t, record, model.TerminationInputTooLarge, model.PhaseInputPreflight)
	if harness.executor.attemptCount() != 0 || harness.executor.checkCount() != 0 {
		t.Fatalf("spawned a reviewer: checks = %d, launches = %d", harness.executor.checkCount(), harness.executor.attemptCount())
	}
	if record.AttemptCount() != 0 {
		t.Fatalf("attempts = %d, want none", record.AttemptCount())
	}
	for _, want := range []string{"input is " + strconv.Itoa(characters) + " characters", "limit is " + strconv.Itoa(characters-1)} {
		if !strings.Contains(record.Termination.Message, want) {
			t.Fatalf("message = %q, want %q", record.Termination.Message, want)
		}
	}
}

func TestOversizedSubjectRecordIsFoundByHistoryTerminationFilter(t *testing.T) {
	repository := unicodeChangedRepository(t)
	characters := measuredPrompt(t, repository)
	harness := newSizeHarness(t, characters-1)
	record, err := harness.conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}

	page, err := harness.ledger.History(store.HistoryQuery{Termination: model.TerminationInputTooLarge, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Entries) != 1 || page.Entries[0].ID != record.ID {
		t.Fatalf("history = %#v, want only %s", page.Entries, record.ID)
	}
}

func TestSubjectAtTheLimitLaunchesAndWarns(t *testing.T) {
	repository := unicodeChangedRepository(t)
	characters := measuredPrompt(t, repository)
	harness := newSizeHarness(t, characters)

	record, err := harness.conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}

	if record.Lifecycle != model.LifecycleCompleted || harness.executor.attemptCount() != 1 {
		t.Fatalf("lifecycle = %q, launches = %d; a subject exactly at the limit must run", record.Lifecycle, harness.executor.attemptCount())
	}
	warnings := harness.warnings.all()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "100%") || !strings.Contains(warnings[0], strconv.Itoa(characters)) {
		t.Fatalf("warnings = %q, want one size warning at 100%%", warnings)
	}
}

func TestSubjectBelowSixtyPercentLaunchesWithoutWarning(t *testing.T) {
	repository := unicodeChangedRepository(t)
	characters := measuredPrompt(t, repository)
	harness := newSizeHarness(t, characters*2)

	if _, err := harness.conductor.Review(context.Background(), testSelection(repository)); err != nil {
		t.Fatal(err)
	}

	if harness.executor.attemptCount() != 1 || len(harness.warnings.all()) != 0 {
		t.Fatalf("launches = %d, warnings = %q", harness.executor.attemptCount(), harness.warnings.all())
	}
}

func TestSubjectAboveSixtyPercentWarnsWithoutBlocking(t *testing.T) {
	repository := unicodeChangedRepository(t)
	characters := measuredPrompt(t, repository)
	harness := newSizeHarness(t, characters*3/2)

	if _, err := harness.conductor.Review(context.Background(), testSelection(repository)); err != nil {
		t.Fatal(err)
	}

	warnings := harness.warnings.all()
	if harness.executor.attemptCount() != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "66%") {
		t.Fatalf("launches = %d, warnings = %q, want one warning at 66%%", harness.executor.attemptCount(), warnings)
	}
}

func TestReviewerWithoutDeclaredLimitIsNeverPreflighted(t *testing.T) {
	repository := unicodeChangedRepository(t)
	harness := newSizeHarness(t, 0)

	if _, err := harness.conductor.Review(context.Background(), testSelection(repository)); err != nil {
		t.Fatal(err)
	}

	if harness.executor.attemptCount() != 1 || len(harness.warnings.all()) != 0 {
		t.Fatalf("launches = %d, warnings = %q", harness.executor.attemptCount(), harness.warnings.all())
	}
}
