package engine

import (
	"testing"

	"reviewparty/internal/model"
)

func TestEvaluateAdjudicationSeparatesQualityCleanBehaviorAndCompletion(t *testing.T) {
	document := model.AdjudicationDocument{SchemaVersion: 1, Cases: []model.EvalCaseAdjudication{
		adjudicatedDefectCase(),
		{CaseID: "clean-zero", ExecutionState: model.EvalCompletedClean, ExpectedFindings: []model.ExpectedFindingAdjudication{}, ReportedFindings: []model.ReportedFindingAdjudication{}},
		{CaseID: "clean-false-positive", ExecutionState: model.EvalCompletedFindings, ExpectedFindings: []model.ExpectedFindingAdjudication{}, ReportedFindings: []model.ReportedFindingAdjudication{{Finding: model.Finding{Ordinal: 1}, Disposition: model.ReportedFalsePositive}}},
		{CaseID: "uncertain", ExecutionState: model.EvalCompletedFindings, ExpectedFindings: []model.ExpectedFindingAdjudication{{Finding: expectedFinding("uncertain"), Disposition: model.ExpectedUncertain}}, ReportedFindings: []model.ReportedFindingAdjudication{{Finding: model.Finding{Ordinal: 1}, Disposition: model.ReportedUncertain}}},
		{CaseID: "incomplete", ExecutionState: model.EvalIncomplete, Termination: &model.ReviewTermination{Category: model.TerminationResultValidationFailure}},
	}}
	score, err := EvaluateAdjudication(document)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, score.DefectRecall, 1, 2)
	assertMetric(t, score.FindingPrecision, 2, 4)
	assertMetric(t, score.CleanCaseAccuracy, 1, 2)
	assertMetric(t, score.CleanFalsePositiveRate, 1, 1)
	assertMetric(t, score.CompletionRate, 4, 5)
	if score.UncertainExpected != 1 {
		t.Fatalf("score = %#v", score)
	}
	if score.UncertainReported != 1 || score.IncompleteCases != 1 {
		t.Fatalf("score = %#v", score)
	}
	if score.TerminationCounts[model.TerminationResultValidationFailure] != 1 {
		t.Fatalf("terminations = %#v", score.TerminationCounts)
	}
}

func TestEvaluateAdjudicationRejectsMissingOrDisagreeingDispositions(t *testing.T) {
	tests := []struct {
		name             string
		caseAdjudication model.EvalCaseAdjudication
	}{
		{name: "missing", caseAdjudication: model.EvalCaseAdjudication{CaseID: "missing", ExecutionState: model.EvalCompletedFindings, ExpectedFindings: []model.ExpectedFindingAdjudication{{Finding: expectedFinding("bug")}}}},
		{name: "disagreeing", caseAdjudication: model.EvalCaseAdjudication{CaseID: "disagreeing", ExecutionState: model.EvalCompletedFindings, ExpectedFindings: []model.ExpectedFindingAdjudication{{Finding: expectedFinding("bug"), Disposition: model.ExpectedMatched, ReportedOrdinal: 1}}, ReportedFindings: []model.ReportedFindingAdjudication{{Finding: model.Finding{Ordinal: 1}, Disposition: model.ReportedMatchedExpected, ExpectedFindingID: "other"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := EvaluateAdjudication(model.AdjudicationDocument{Cases: []model.EvalCaseAdjudication{test.caseAdjudication}}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func adjudicatedDefectCase() model.EvalCaseAdjudication {
	return model.EvalCaseAdjudication{
		CaseID: "defect", ExecutionState: model.EvalCompletedFindings,
		ExpectedFindings: []model.ExpectedFindingAdjudication{
			{Finding: expectedFinding("matched"), Disposition: model.ExpectedMatched, ReportedOrdinal: 1},
			{Finding: expectedFinding("missed"), Disposition: model.ExpectedMissed},
		},
		ReportedFindings: []model.ReportedFindingAdjudication{
			{Finding: model.Finding{Ordinal: 1}, Disposition: model.ReportedMatchedExpected, ExpectedFindingID: "matched"},
			{Finding: model.Finding{Ordinal: 2}, Disposition: model.ReportedNovelValid},
			{Finding: model.Finding{Ordinal: 3}, Disposition: model.ReportedFalsePositive},
		},
	}
}

func expectedFinding(id string) model.ExpectedFinding {
	return model.ExpectedFinding{ID: id, Behavior: "behavior", Impact: "impact", Evidence: []string{"evidence"}}
}

func assertMetric(t *testing.T, metric model.RatioMetric, numerator, denominator int) {
	t.Helper()
	if metric.Numerator != numerator {
		t.Fatalf("metric = %#v, want %d/%d", metric, numerator, denominator)
	}
	if metric.Denominator != denominator {
		t.Fatalf("metric = %#v, want %d/%d", metric, numerator, denominator)
	}
	if denominator == 0 {
		return
	}
	if metric.Value == nil || *metric.Value != float64(numerator)/float64(denominator) {
		t.Fatalf("metric value = %#v", metric.Value)
	}
}
