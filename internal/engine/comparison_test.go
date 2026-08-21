package engine

import (
	"errors"
	"testing"

	"reviewparty/internal/model"
)

func TestCompareEvalExperimentsReportsExactDeltasAndRuntime(t *testing.T) {
	baseline := comparisonFixture(
		comparisonCaseFixture{"case-a", "digest-a", model.EvalCompletedFindings, true, false, 100},
		comparisonCaseFixture{"case-b", "digest-b", model.EvalCompletedFindings, false, false, 200},
		comparisonCaseFixture{"case-clean", "digest-clean", model.EvalCompletedClean, false, false, 300},
	)
	candidate := comparisonFixture(
		comparisonCaseFixture{"case-a", "digest-a", model.EvalCompletedFindings, true, false, 110},
		comparisonCaseFixture{"case-b", "digest-b", model.EvalCompletedFindings, true, true, 220},
		comparisonCaseFixture{"case-clean", "digest-clean", model.EvalCompletedClean, false, true, 330},
	)
	comparison, err := CompareEvalExperiments(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	assertComparisonMetric(t, comparison.DefectRecall, comparisonMetricExpectation{ratioExpectation{1, 2}, ratioExpectation{2, 2}, 0.5})
	assertComparisonMetric(t, comparison.FindingPrecision, comparisonMetricExpectation{ratioExpectation{1, 1}, ratioExpectation{2, 4}, -0.5})
	assertComparisonMetric(t, comparison.CleanCaseAccuracy, comparisonMetricExpectation{ratioExpectation{1, 1}, ratioExpectation{0, 1}, -1})
	if comparison.Coverage.ComparedCases != 3 {
		t.Fatalf("coverage = %#v", comparison.Coverage)
	}
	if len(comparison.Coverage.ComparedCaseIDs) != 3 {
		t.Fatalf("coverage ids = %#v", comparison.Coverage.ComparedCaseIDs)
	}
	assertComparisonRuntime(t, comparison)
}

func TestCompareEvalExperimentsPartitionsMismatchedCases(t *testing.T) {
	baseline := comparisonFixture(
		comparisonCaseFixture{"shared", "same", model.EvalCompletedClean, false, false, 10},
		comparisonCaseFixture{"changed", "old", model.EvalCompletedClean, false, false, 20},
	)
	candidate := comparisonFixture(
		comparisonCaseFixture{"shared", "same", model.EvalCompletedClean, false, false, 15},
		comparisonCaseFixture{"changed", "new", model.EvalCompletedClean, false, false, 25},
		comparisonCaseFixture{"candidate-only", "new", model.EvalCompletedClean, false, false, 30},
	)
	comparison, err := CompareEvalExperiments(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Coverage.ComparedCases != 1 || comparison.Coverage.ComparedCaseIDs[0] != "shared" {
		t.Fatalf("coverage = %#v", comparison.Coverage)
	}
	if len(comparison.Coverage.MismatchedCaseIDs) != 1 || comparison.Coverage.MismatchedCaseIDs[0] != "changed" {
		t.Fatalf("mismatched = %#v", comparison.Coverage.MismatchedCaseIDs)
	}
	if len(comparison.Coverage.OmittedCandidateIDs) != 1 || comparison.Coverage.OmittedCandidateIDs[0] != "candidate-only" {
		t.Fatalf("omitted candidate = %#v", comparison.Coverage.OmittedCandidateIDs)
	}
}

func TestCompareEvalExperimentsKeepsIncompleteOutOfQualityMetrics(t *testing.T) {
	baseline := comparisonFixture(comparisonCaseFixture{"case-a", "digest-a", model.EvalCompletedFindings, true, false, 10})
	candidate := comparisonFixture(comparisonCaseFixture{"case-a", "digest-a", model.EvalIncomplete, false, false, 40})
	candidate.Cases["case-a"] = comparisonCase{ID: "case-a", Digest: "digest-a", RuntimeMS: 40, Adjudication: model.EvalCaseAdjudication{CaseID: "case-a", ExecutionState: model.EvalIncomplete, Termination: &model.ReviewTermination{Category: model.TerminationResultValidationFailure}}}
	comparison, err := CompareEvalExperiments(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.DefectRecall.Candidate.Denominator != 0 {
		t.Fatalf("candidate quality/completion = %#v / %#v", comparison.DefectRecall.Candidate, comparison.CompletionRate.Candidate)
	}
	if comparison.CompletionRate.Candidate.Numerator != 0 {
		t.Fatalf("candidate completion numerator = %#v", comparison.CompletionRate.Candidate)
	}
	if comparison.CompletionRate.Candidate.Denominator != 1 {
		t.Fatalf("candidate completion denominator = %#v", comparison.CompletionRate.Candidate)
	}
	if comparison.CandidateTerminations[model.TerminationResultValidationFailure] != 1 {
		t.Fatalf("terminations = %#v", comparison.CandidateTerminations)
	}
}

func TestCompareEvalExperimentsRejectsDifferentRetryPolicies(t *testing.T) {
	baseline := comparisonFixture(comparisonCaseFixture{"case-a", "digest-a", model.EvalCompletedClean, false, false, 10})
	candidate := comparisonFixture(comparisonCaseFixture{"case-a", "digest-a", model.EvalCompletedClean, false, false, 10})
	baseline.Identity.Experiment.RetryPolicy.MaxAttempts = 1
	candidate.Identity.Experiment.RetryPolicy.MaxAttempts = 3
	if _, err := CompareEvalExperiments(baseline, candidate); !errors.Is(err, ErrIncompatibleComparison) {
		t.Fatalf("error = %v", err)
	}
}

type comparisonCaseFixture struct {
	id, digest             string
	state                  model.EvalExecutionState
	matched, falsePositive bool
	runtimeMS              int64
}

func comparisonFixture(fixtures ...comparisonCaseFixture) comparisonSide {
	cases := make(map[string]comparisonCase, len(fixtures))
	documentCases := make([]model.EvalCaseAdjudication, 0, len(fixtures))
	for _, fixture := range fixtures {
		adjudication := comparisonAdjudication(fixture)
		cases[fixture.id] = comparisonCase{ID: fixture.id, Digest: fixture.digest, Adjudication: adjudication, RuntimeMS: fixture.runtimeMS}
		documentCases = append(documentCases, adjudication)
	}
	return comparisonSide{Cases: cases, Revision: model.AdjudicationRevision{Document: model.AdjudicationDocument{SchemaVersion: 1, Cases: documentCases}}, Identity: model.ComparisonIdentity{Suite: "suite", SuiteRevision: "v1", SuiteDigest: "digest", Experiment: model.ExperimentConfiguration{Profile: "bugs", Reviewer: "opencode", Model: "model", Effort: "high", Deadline: "1m", RetryPolicy: model.RetryPolicy{MaxAttempts: 1, InitialBackoff: "1s", MaxBackoff: "1s"}, ConcurrencyLimit: 1}}}
}

func comparisonAdjudication(fixture comparisonCaseFixture) model.EvalCaseAdjudication {
	adjudication := model.EvalCaseAdjudication{CaseID: fixture.id, ExecutionState: fixture.state, ExpectedFindings: []model.ExpectedFindingAdjudication{}, ReportedFindings: []model.ReportedFindingAdjudication{}}
	if fixture.state == model.EvalCompletedFindings {
		return comparisonFindingAdjudication(adjudication, fixture)
	}
	return comparisonNonFindingAdjudication(adjudication, fixture)
}

func comparisonFindingAdjudication(adjudication model.EvalCaseAdjudication, fixture comparisonCaseFixture) model.EvalCaseAdjudication {
	expected := model.ExpectedFindingAdjudication{Finding: model.ExpectedFinding{ID: "bug-" + fixture.id, Behavior: "behavior", Impact: "impact", Evidence: []string{"evidence"}}, Disposition: model.ExpectedMissed}
	if fixture.matched {
		expected.Disposition = model.ExpectedMatched
		expected.ReportedOrdinal = 1
		adjudication.ReportedFindings = append(adjudication.ReportedFindings, model.ReportedFindingAdjudication{Finding: model.Finding{Ordinal: 1}, Disposition: model.ReportedMatchedExpected, ExpectedFindingID: expected.Finding.ID})
	}
	adjudication.ExpectedFindings = append(adjudication.ExpectedFindings, expected)
	return comparisonFalsePositive(adjudication, fixture.falsePositive)
}

func comparisonNonFindingAdjudication(adjudication model.EvalCaseAdjudication, fixture comparisonCaseFixture) model.EvalCaseAdjudication {
	return comparisonFalsePositive(adjudication, fixture.falsePositive)
}

func comparisonFalsePositive(adjudication model.EvalCaseAdjudication, include bool) model.EvalCaseAdjudication {
	if include {
		adjudication.ReportedFindings = append(adjudication.ReportedFindings, model.ReportedFindingAdjudication{Finding: model.Finding{Ordinal: len(adjudication.ReportedFindings) + 1}, Disposition: model.ReportedFalsePositive})
	}
	return adjudication
}

type ratioExpectation struct {
	numerator, denominator int
}

type comparisonMetricExpectation struct {
	baseline, candidate ratioExpectation
	delta               float64
}

func assertComparisonMetric(t *testing.T, metric model.ComparisonMetric, expected comparisonMetricExpectation) {
	t.Helper()
	assertRatioMetric(t, metric.Baseline, expected.baseline)
	assertRatioMetric(t, metric.Candidate, expected.candidate)
	if metric.Delta == nil {
		t.Fatalf("delta is nil, want %f", expected.delta)
	}
	if *metric.Delta != expected.delta {
		t.Fatalf("delta = %f, want %f", *metric.Delta, expected.delta)
	}
}

func assertRatioMetric(t *testing.T, metric model.RatioMetric, expected ratioExpectation) {
	t.Helper()
	if metric.Numerator != expected.numerator {
		t.Fatalf("numerator = %d, want %d", metric.Numerator, expected.numerator)
	}
	if metric.Denominator != expected.denominator {
		t.Fatalf("denominator = %d, want %d", metric.Denominator, expected.denominator)
	}
}

func assertComparisonRuntime(t *testing.T, comparison model.EvalComparison) {
	t.Helper()
	if comparison.BaselineRuntime.TotalMS != 600 {
		t.Fatalf("baseline runtime = %#v", comparison.BaselineRuntime)
	}
	if comparison.CandidateRuntime.TotalMS != 660 {
		t.Fatalf("candidate runtime = %#v", comparison.CandidateRuntime)
	}
	if comparison.RuntimeDeltaMS != 60 {
		t.Fatalf("runtime delta = %d", comparison.RuntimeDeltaMS)
	}
}
