package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestSeededEvalRunsThroughOrdinaryReviewWithDurableProvenance(t *testing.T) {
	suite, sourceFile := writeSeededEvalTestSuite(t, seededTestCase{expectedFiles: []string{"service.go"}, patch: seededPatch(false)})
	original := readTestFile(t, sourceFile)
	conductor := testEvalConductor(t, successfulExecutor(findingsReview))
	run, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err != nil {
		t.Fatal(err)
	}
	evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	assertSeedRevision(t, evalRun.Case)
	review, err := conductor.Inspect(context.Background(), evalRun.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	assertSeededReview(t, review)
	if after := readTestFile(t, sourceFile); after != original {
		t.Fatal("seed materialization modified the source fixture")
	}
}

func TestSeededEvalRejectsSourceCommitMismatchBeforeLaunch(t *testing.T) {
	assertSeedPreflightFailure(t, seededTestCase{expectedFiles: []string{"service.go"}, sourceCommit: strings.Repeat("0", 40), patch: seededPatch(false)}, "source commit mismatch")
}

func TestSeededEvalRejectsUndeclaredChangedPathsBeforeLaunch(t *testing.T) {
	assertSeedPreflightFailure(t, seededTestCase{expectedFiles: []string{"service.go"}, patch: seededPatch(true)}, "changed files")
}

func TestPackagedSeededEvalRunsThroughOrdinaryReview(t *testing.T) {
	conductor := testEvalConductor(t, successfulExecutor(findingsReview))
	run, err := conductor.RunEvalSuite(testContext(t), evalSelection("global:seeded-bugs"))
	if err != nil {
		t.Fatal(err)
	}
	evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if evalRun.Case.Seed == nil || evalRun.Case.Seed.SourceCommit != "d8b458cf667f0b9d20d90d3a1b02efd2989017f3" {
		t.Fatalf("seeded Eval Run = %#v", evalRun)
	}
	if evalRun.ExecutionState != model.EvalCompletedFindings {
		t.Fatalf("execution state = %s", evalRun.ExecutionState)
	}
	if _, err := conductor.ExportAdjudication(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
}

type seededTestCase struct {
	expectedFiles []string
	patch         string
	sourceCommit  string
}

type seededTestPaths struct {
	root     string
	caseRoot string
}

func assertSeedPreflightFailure(t *testing.T, fixture seededTestCase, message string) {
	t.Helper()
	suite, _ := writeSeededEvalTestSuite(t, fixture)
	executor := successfulExecutor(cleanReview)
	conductor := testEvalConductor(t, executor)
	_, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("error = %v", err)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
}

func assertSeedRevision(t *testing.T, revision model.EvalCaseRevision) {
	t.Helper()
	if revision.Mode != "seeded" || revision.Seed == nil {
		t.Fatalf("seed revision = %#v", revision)
	}
	if revision.Seed.ID != "ignored-error" || revision.Seed.PatchDigest == "" {
		t.Fatalf("seed provenance = %#v", revision.Seed)
	}
}

func assertSeededReview(t *testing.T, review model.ReviewRecord) {
	t.Helper()
	if !strings.Contains(review.Subject.Patch, "return nil") {
		t.Fatalf("ordinary Review did not contain seed: %s", review.Subject.Patch)
	}
	if review.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("review lifecycle = %s", review.Lifecycle)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func writeSeededEvalTestSuite(t *testing.T, fixture seededTestCase) (string, string) {
	t.Helper()
	root := t.TempDir()
	caseRoot := filepath.Join(root, "cases", "seeded")
	base := filepath.Join(caseRoot, "base")
	service := filepath.Join(base, "service.go")
	writeEvalFile(t, service, "package fixture\n\nfunc Store() error {\n\treturn save()\n}\n\nfunc save() error { return nil }\n")
	writeEvalFile(t, filepath.Join(base, "helper.go"), "package fixture\n\nconst enabled = true\n")
	fixture.sourceCommit = seededSourceCommit(t, base, fixture.sourceCommit)
	writeEvalFile(t, filepath.Join(caseRoot, "seed.patch"), fixture.patch)
	writeSeededDefinitions(t, seededTestPaths{root: root, caseRoot: caseRoot}, fixture)
	return root, service
}

func seededSourceCommit(t *testing.T, base, sourceCommit string) string {
	t.Helper()
	if sourceCommit != "" {
		return sourceCommit
	}
	repository := seedRepository(base)
	if err := initializeSeedRepository(repository); err != nil {
		t.Fatal(err)
	}
	commit, err := repository.output("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(base, ".git")); err != nil {
		t.Fatal(err)
	}
	return commit
}

func writeSeededDefinitions(t *testing.T, paths seededTestPaths, fixture seededTestCase) {
	t.Helper()
	definition := map[string]any{
		"schema_version": 1, "id": "seeded-ignored-error", "mode": "seeded", "classification": "defect", "base": "base",
		"seed":              map[string]any{"id": "ignored-error", "source_commit": fixture.sourceCommit, "patch": "seed.patch", "expected_files": fixture.expectedFiles},
		"expected_findings": []map[string]any{{"id": "ignored-save-error", "behavior": "Store ignores a save failure.", "impact": "The caller observes success after persistence failed.", "evidence": []string{"Store returns nil instead of the save error."}}},
	}
	writeJSONTestFile(t, filepath.Join(paths.caseRoot, "case.json"), definition)
	writeJSONTestFile(t, filepath.Join(paths.root, "suite.json"), map[string]any{"schema_version": 1, "name": "seeded-test", "revision": "v1", "cases": []string{"cases/seeded/case.json"}})
}

func writeJSONTestFile(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeEvalFile(t, path, string(payload))
}

func seededPatch(includeHelper bool) string {
	patch := "diff --git a/service.go b/service.go\nindex f6c448a..fba1f43 100644\n--- a/service.go\n+++ b/service.go\n@@ -1,7 +1,7 @@\n package fixture\n \n func Store() error {\n-\treturn save()\n+\treturn nil\n }\n \n func save() error { return nil }\n"
	if includeHelper {
		patch += "diff --git a/helper.go b/helper.go\nindex 944ef0c..2d96dc3 100644\n--- a/helper.go\n+++ b/helper.go\n@@ -1,3 +1,3 @@\n package fixture\n \n-const enabled = true\n+const enabled = false\n"
	}
	return patch
}
