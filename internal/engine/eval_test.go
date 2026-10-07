package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/artifact"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestEvalPreflightRejectsInvalidCaseBeforeHarnessLaunch(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "invalid", unknownField: true}})
	executor := successfulExecutor(cleanReview)
	conductor := testEvalConductor(t, executor)
	_, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
}

func TestEvalRecordsOrdinaryReviewsForEachExecutionCategory(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "clean"}, {id: "findings"}, {id: "incomplete"}})
	executor := &evalSequenceExecutor{outputs: []string{cleanReview, findingsReview, "not a result contract"}}
	conductor := testEvalConductor(t, executor)
	run, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err != nil {
		t.Fatal(err)
	}
	assertEvalExecutionCounts(t, run)
	if run.Lifecycle != model.LifecycleCompleted || run.Termination != nil {
		t.Fatalf("suite lifecycle = %s, termination = %#v", run.Lifecycle, run.Termination)
	}
	assertNoPromptPatchMentions(t, executor.prompts, filepath.Dir(suite))
	for index, id := range run.EvalRunIDs {
		assertPersistedEvalRun(t, persistedEvalAssertion{conductor: conductor, suite: suite, index: index, id: id})
	}
	if executor.sawGit || executor.sawExpected {
		t.Fatalf("reviewer view leaked corpus authority: %#v", executor)
	}
}

func TestEvalRetriesTransientFailureInsideSameReview(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "retry"}})
	executor := &scriptedEvalExecutor{executions: []attemptExecution{
		{Outcome: model.AttemptTransientFailure, FailureCategory: model.TerminationTransportFailure, FailurePhase: model.PhaseReviewerExecution, Diagnostic: "temporary transport failure"},
		{Outcome: model.AttemptCompleted, AssistantText: cleanReview},
	}}
	conductor := testEvalConductor(t, executor)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := evalSelection(suite)
	selection.Experiment.RetryPolicy.MaxAttempts = 3
	run, err := conductor.RunEvalSuite(testContext(t), selection)
	if err != nil {
		t.Fatal(err)
	}
	evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	review, err := conductor.Inspect(context.Background(), evalRun.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	assertRetriedReview(t, review, evalRun)
}

func assertRetriedReview(t *testing.T, review model.ReviewRecord, evalRun model.EvalRun) {
	t.Helper()
	if review.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("review lifecycle = %s", review.Lifecycle)
	}
	if review.AttemptCount() != 2 {
		t.Fatalf("attempts = %#v", review.Passes[0].Attempts)
	}
	if evalRun.ExecutionState != model.EvalCompletedClean {
		t.Fatalf("Eval state = %s", evalRun.ExecutionState)
	}
	if review.Passes[0].Attempts[0].Outcome != model.AttemptTransientFailure {
		t.Fatalf("first attempt = %#v", review.Passes[0].Attempts[0])
	}
	if review.Passes[0].Attempts[1].Outcome != model.AttemptCompleted {
		t.Fatalf("second attempt = %#v", review.Passes[0].Attempts[1])
	}
}

func TestEvalDoesNotRetryAuthenticationFailure(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "authentication"}})
	executor := &scriptedEvalExecutor{executions: []attemptExecution{{Outcome: model.AttemptUnknownFailure, FailureCategory: model.TerminationAuthenticationFailure, FailurePhase: model.PhaseReviewerExecution, Diagnostic: "invalid credential"}}}
	conductor := testEvalConductor(t, executor)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := evalSelection(suite)
	selection.Experiment.RetryPolicy.MaxAttempts = 3
	run, err := conductor.RunEvalSuite(testContext(t), selection)
	if err != nil {
		t.Fatal(err)
	}
	evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	review, err := conductor.Inspect(context.Background(), evalRun.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.AttemptCount() != 1 || evalRun.ExecutionState != model.EvalIncomplete {
		t.Fatalf("attempts=%d eval=%s", review.AttemptCount(), evalRun.ExecutionState)
	}
}

func TestEvalContinuesAfterRetryExhaustion(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "exhausted"}, {id: "later"}})
	executor := &scriptedEvalExecutor{executions: []attemptExecution{
		{Outcome: model.AttemptTransientFailure, FailureCategory: model.TerminationTransportFailure, FailurePhase: model.PhaseReviewerExecution, Diagnostic: "temporary one"},
		{Outcome: model.AttemptTransientFailure, FailureCategory: model.TerminationTransportFailure, FailurePhase: model.PhaseReviewerExecution, Diagnostic: "temporary two"},
		{Outcome: model.AttemptCompleted, AssistantText: cleanReview},
	}}
	conductor := testEvalConductor(t, executor)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := evalSelection(suite)
	selection.Experiment.RetryPolicy.MaxAttempts = 2
	run, err := conductor.RunEvalSuite(testContext(t), selection)
	if err != nil {
		t.Fatal(err)
	}
	first, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionState != model.EvalIncomplete {
		t.Fatalf("first = %s", first.ExecutionState)
	}
	if second.ExecutionState != model.EvalCompletedClean {
		t.Fatalf("second = %s", second.ExecutionState)
	}
	if run.IncompleteCount != 1 || run.CompletedCleanCount != 1 {
		t.Fatalf("counts=%d/%d", run.IncompleteCount, run.CompletedCleanCount)
	}
}

func TestEvalConcurrencyLimitBoundsActiveReviewersAndPreservesManifestOrder(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}, {id: "third"}})
	executor := &boundedEvalExecutor{delays: []time.Duration{40 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}}
	conductor := testEvalConductor(t, executor)
	selection := evalSelection(suite)
	selection.Experiment.ConcurrencyLimit = 2
	run, err := conductor.RunEvalSuite(testContext(t), selection)
	if err != nil {
		t.Fatal(err)
	}
	if executor.peak != 2 {
		t.Fatalf("peak active = %d, want 2", executor.peak)
	}
	for index, want := range []string{"first", "second", "third"} {
		evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[index])
		if err != nil {
			t.Fatal(err)
		}
		if evalRun.Case.ID != want {
			t.Fatalf("run %d case = %q, want %q", index, evalRun.Case.ID, want)
		}
	}
}

func TestEvalConcurrencyLimitOneRemainsSequential(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
	executor := &boundedEvalExecutor{delays: []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}}
	conductor := testEvalConductor(t, executor)
	if _, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite)); err != nil {
		t.Fatal(err)
	}
	if executor.peak != 1 {
		t.Fatalf("peak active = %d, want 1", executor.peak)
	}
}

type boundedEvalExecutor struct {
	mu     sync.Mutex
	delays []time.Duration
	next   int
	active int
	peak   int
}

func (executor *boundedEvalExecutor) Check(context.Context, reviewerCandidate) availability {
	return availability{Available: true}
}

func (executor *boundedEvalExecutor) Execute(context.Context, attemptSpec) attemptExecution {
	executor.mu.Lock()
	index := executor.next
	executor.next++
	executor.active++
	if executor.active > executor.peak {
		executor.peak = executor.active
	}
	delay := executor.delays[index]
	executor.mu.Unlock()
	time.Sleep(delay)
	executor.mu.Lock()
	executor.active--
	executor.mu.Unlock()
	return attemptExecution{Outcome: model.AttemptCompleted, AssistantText: cleanReview}
}

type scriptedEvalExecutor struct {
	mu         sync.Mutex
	executions []attemptExecution
}

func (executor *scriptedEvalExecutor) Check(context.Context, reviewerCandidate) availability {
	return availability{Available: true}
}

func (executor *scriptedEvalExecutor) Execute(context.Context, attemptSpec) attemptExecution {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	execution := executor.executions[0]
	executor.executions = executor.executions[1:]
	return execution
}

func assertEvalExecutionCounts(t *testing.T, run model.EvalSuiteRun) {
	t.Helper()
	if run.CompletedCleanCount != 1 {
		t.Fatalf("clean count = %d", run.CompletedCleanCount)
	}
	if run.CompletedFindingCount != 1 {
		t.Fatalf("finding count = %d", run.CompletedFindingCount)
	}
	if run.IncompleteCount != 1 {
		t.Fatalf("incomplete count = %d", run.IncompleteCount)
	}
}

type persistedEvalAssertion struct {
	conductor *Conductor
	suite     string
	index     int
	id        model.EvalRunID
}

func assertPersistedEvalRun(t *testing.T, assertion persistedEvalAssertion) {
	t.Helper()
	evalRun, err := assertion.conductor.InspectEvalRun(context.Background(), assertion.id)
	if err != nil {
		t.Fatal(err)
	}
	review, err := assertion.conductor.Inspect(context.Background(), evalRun.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	assertSyntheticSubject(t, assertion.index, review.Subject)
	if evalRun.AdjudicationState != "awaiting_adjudication" {
		t.Fatalf("adjudication = %s", evalRun.AdjudicationState)
	}
	if evalRun.Case.Digest == "" {
		t.Fatal("case digest is empty")
	}
}

func assertSyntheticSubject(t *testing.T, index int, subject model.ReviewSubject) {
	t.Helper()
	if subject.Kind != model.SubjectCapturedChange {
		t.Fatalf("case %d kind = %s", index, subject.Kind)
	}
	if subject.Repository != "eval://"+subject.Identity {
		t.Fatalf("case %d repository = %s", index, subject.Repository)
	}
}

func assertNoPromptPatchMentions(t *testing.T, prompts []string, authority string) {
	t.Helper()
	for index, prompt := range prompts {
		if patch := promptPatch(t, prompt); strings.Contains(patch, authority) {
			t.Fatalf("prompt %d patch leaked %q: %s", index, authority, patch)
		}
	}
}

func TestEvalRerunPreservesIndependentSuiteHistory(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "case-one"}})
	conductor := testEvalConductor(t, successfulExecutor(cleanReview))
	first, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err != nil {
		t.Fatal(err)
	}
	second, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.EvalRunIDs[0] == second.EvalRunIDs[0] {
		t.Fatal("rerun overwrote Eval history")
	}
	if _, err := conductor.InspectEvalSuiteRun(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := conductor.InspectEvalSuiteRun(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPackagedEvalSuitesSelectDistinctCorpora(t *testing.T) {
	canary := runPackagedEval(t, "global:canary-bugs", successfulExecutor(cleanReview))
	general := runPackagedEval(t, "global:general-bugs", successfulExecutor(cleanReview))
	if canary.Suite != "global:canary-bugs" || canary.SuiteRevision != "canary-bugs-v1" {
		t.Fatalf("canary suite = %s@%s", canary.Suite, canary.SuiteRevision)
	}
	if general.Suite != "global:general-bugs" || general.SuiteRevision != "general-bugs-v2" {
		t.Fatalf("general suite = %s@%s", general.Suite, general.SuiteRevision)
	}
	if len(canary.EvalRunIDs) == len(general.EvalRunIDs) || canary.SuiteDigest == general.SuiteDigest {
		t.Fatal("packaged suite selection resolved the same corpus")
	}
}

func TestPackagedEvalCorpusIsIgnoredByRecursiveGoDiscovery(t *testing.T) {
	root := goModuleRoot(t)
	command := exec.Command("go", "list", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOCACHE="+t.TempDir())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list ./...: %v\n%s", err, output)
	}
}

func TestCallerOwnedEvalPreservesGoModText(t *testing.T) {
	root := writeEvalTestSuite(t, []testEvalCase{{id: "caller-owned"}})
	for _, fixture := range []string{"base", "head"} {
		writeEvalFile(t, filepath.Join(root, "cases", "caller-owned", fixture, "go.mod.txt"), "module example.com/caller-owned\n")
	}

	suite, err := loadEvalSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(checkedCleanup(t, "cleanup eval suite", suite.cleanup))
	for _, fixture := range []string{suite.cases[0].base, suite.cases[0].head} {
		if _, err := os.Stat(filepath.Join(fixture, "go.mod.txt")); err != nil {
			t.Fatalf("caller-owned fixture lost go.mod.txt: %v", err)
		}
		if _, err := os.Stat(filepath.Join(fixture, "go.mod")); !os.IsNotExist(err) {
			t.Fatalf("caller-owned fixture gained go.mod: %v", err)
		}
	}
}

func TestPackagedCodeQualityEvalSuiteLoadsDeclaredCases(t *testing.T) {
	suite, err := loadEvalSuite("global:code-quality")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(checkedCleanup(t, "cleanup eval suite", suite.cleanup))
	if suite.name != "global:code-quality" || suite.revision != "code-quality-v1" {
		t.Fatalf("suite identity = %q@%q", suite.name, suite.revision)
	}
	if len(suite.cases) != 6 {
		t.Fatalf("case count = %d, want 6", len(suite.cases))
	}
	if suite.cases[0].revision.ID != "dispatcher-grows-feature-branch" || suite.cases[4].revision.Classification != "known_clean" {
		t.Fatalf("case revisions = %#v", suite.cases)
	}
}

func TestCodeQualityEvalRejectsMismatchedProfileBeforeLaunch(t *testing.T) {
	executor := successfulExecutor(cleanReview)
	conductor := testEvalConductor(t, executor)
	selection := evalSelection("global:code-quality")
	selection.Experiment.Profile = "bugs"
	if _, err := conductor.RunEvalSuite(testContext(t), selection); err == nil || !strings.Contains(err.Error(), "requires the code-quality Profile") {
		t.Fatalf("error = %v", err)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want 0", executor.attemptCount())
	}
}

func TestCodeQualityEvalSuiteAcceptsScopedCodeQualityProfile(t *testing.T) {
	tests := []struct {
		profile string
		wantErr bool
	}{
		{"code-quality", false},
		{"global:code-quality", false},
		{"global:bugs", true},
		{"bugs", true},
	}
	for _, test := range tests {
		err := validateEvalSuiteProfile("global:code-quality", test.profile)
		if (err != nil) != test.wantErr {
			t.Fatalf("profile %q error = %v, want error %t", test.profile, err, test.wantErr)
		}
	}
}

func TestGeneralEvalReviewerReceivesMultiFileRepositoryWithoutAuthority(t *testing.T) {
	executor := &evalSequenceExecutor{outputs: []string{cleanReview, cleanReview, cleanReview, cleanReview, cleanReview, cleanReview}}
	conductor := testEvalConductor(t, executor)
	run, err := conductor.RunEvalSuite(testContext(t), evalSelection("global:general-bugs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.fileCounts) == 0 || executor.fileCounts[0] < 5 {
		t.Fatalf("reviewer file counts = %#v", executor.fileCounts)
	}
	if executor.sawAuthority {
		t.Fatal("reviewer view leaked suite or case authority")
	}
	if !executor.sawGoModule {
		t.Fatal("reviewer view did not materialize the packaged Go module")
	}
	assertFirstGeneralSubject(t, conductor, run)
	assertNoPromptPatchMentions(t, executor.prompts, "persisted-zero-role-becomes-administrator")
}

func runPackagedEval(t *testing.T, suite string, executor attemptExecutor) model.EvalSuiteRun {
	t.Helper()
	conductor := testEvalConductor(t, executor)
	run, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func assertFirstGeneralSubject(t *testing.T, conductor *Conductor, run model.EvalSuiteRun) {
	t.Helper()
	evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	review, err := conductor.Inspect(context.Background(), evalRun.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Subject.ChangedPaths) < 2 {
		t.Fatalf("changed paths = %#v", review.Subject.ChangedPaths)
	}
}

type evalSequenceExecutor struct {
	mu           sync.Mutex
	outputs      []string
	sawGit       bool
	sawExpected  bool
	sawAuthority bool
	sawGoModule  bool
	fileCounts   []int
	prompts      []string
	onExecute    func()
}

func (executor *evalSequenceExecutor) Check(context.Context, reviewerCandidate) availability {
	return availability{Available: true}
}

func (executor *evalSequenceExecutor) Execute(_ context.Context, spec attemptSpec) attemptExecution {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.onExecute != nil {
		executor.onExecute()
	}
	executor.prompts = append(executor.prompts, spec.Prompt)
	if _, err := os.Stat(filepath.Join(spec.Repository, ".git")); err == nil {
		executor.sawGit = true
	}
	if _, err := os.Stat(filepath.Join(spec.Repository, "expected.json")); err == nil {
		executor.sawExpected = true
	}
	count, authority, goModule, err := inspectReviewerRepository(spec.Repository)
	if err != nil {
		return failedExecution(model.AttemptUnknownFailure, model.TerminationUnknownFailure, model.PhaseReviewerExecution, err.Error())
	}
	executor.fileCounts = append(executor.fileCounts, count)
	executor.sawAuthority = executor.sawAuthority || authority
	executor.sawGoModule = executor.sawGoModule || goModule
	output := executor.outputs[0]
	executor.outputs = executor.outputs[1:]
	return attemptExecution{AssistantText: output, Outcome: model.AttemptCompleted}
}

func inspectReviewerRepository(repository string) (int, bool, bool, error) {
	count := 0
	authority := false
	goModule := false
	if err := filepath.WalkDir(repository, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		count++
		name := entry.Name()
		if evalAuthorityFile(name) {
			authority = true
		}
		if name == "go.mod" {
			goModule = true
		}
		return nil
	}); err != nil {
		return 0, false, false, err
	}
	return count, authority, goModule, nil
}

func evalAuthorityFile(name string) bool {
	switch name {
	case "case.json", "suite.json", "expected.json":
		return true
	default:
		return false
	}
}

func testEvalConductor(t *testing.T, executor attemptExecutor) *Conductor {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(checkedCleanup(t, "close ledger", ledger.Close))
	conductor, err := newConductorWithManager(ledger, catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}), newTestConfigurationManager(t), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conductor.artifacts, err = artifact.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return conductor
}

func evalSelection(suite string) model.EvalSuiteSelection {
	return model.EvalSuiteSelection{Suite: suite, Experiment: model.ExperimentConfiguration{Profile: "bugs", Reviewer: defaultReviewer, Model: "grok-code-fast-1", Effort: "high", Deadline: time.Second.String(), RetryPolicy: model.RetryPolicy{MaxAttempts: 1, InitialBackoff: "1ms", MaxBackoff: "1ms"}, ConcurrencyLimit: 1}}
}

type testEvalCase struct {
	id           string
	unknownField bool
}

func writeEvalTestSuite(t *testing.T, cases []testEvalCase) string {
	t.Helper()
	root := t.TempDir()
	casePaths := make([]string, 0, len(cases))
	for _, evalCase := range cases {
		caseDirectory := filepath.Join(root, "cases", evalCase.id)
		writeEvalFile(t, filepath.Join(caseDirectory, "base", "value.go"), "package fixture\n\nconst Value = 1\n")
		writeEvalFile(t, filepath.Join(caseDirectory, "head", "value.go"), "package fixture\n\nconst Value = 2\n")
		definition := map[string]any{"schema_version": 1, "id": evalCase.id, "mode": "change", "classification": "defect", "base": "base", "head": "head", "expected_findings": []map[string]any{{"id": "changed-value", "behavior": "The value changed.", "impact": "A caller observes another value.", "evidence": []string{"The constant differs."}}}}
		if evalCase.unknownField {
			definition["surprise"] = true
		}
		payload, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		writeEvalFile(t, filepath.Join(caseDirectory, "case.json"), string(payload))
		casePaths = append(casePaths, filepath.ToSlash(filepath.Join("cases", evalCase.id, "case.json")))
	}
	manifest, err := json.Marshal(map[string]any{"schema_version": 1, "name": "test-suite", "revision": "v1", "cases": casePaths})
	if err != nil {
		t.Fatal(err)
	}
	writeEvalFile(t, filepath.Join(root, "suite.json"), string(manifest))
	return root
}

func writeEvalFile(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func goModuleRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not find go.mod")
		}
		directory = parent
	}
}

func TestEvalRejectsRepositoryScopedProfileBecauseEvalResolvesGlobalOnly(t *testing.T) {
	executor := successfulExecutor(cleanReview)
	conductor := testEvalConductor(t, executor)
	selection := evalSelection("global:code-quality")
	selection.Experiment.Profile = "repository:code-quality"
	_, err := conductor.RunEvalSuite(testContext(t), selection)
	if err == nil || !strings.Contains(err.Error(), "resolve from global Configuration") {
		t.Fatalf("error = %v, want the global-only eval Profile error", err)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want 0", executor.attemptCount())
	}
}
