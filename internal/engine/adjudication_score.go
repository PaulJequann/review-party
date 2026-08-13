package engine

import (
	"fmt"

	"reviewparty/internal/model"
)

func EvaluateAdjudication(document model.AdjudicationDocument) (model.EvalScore, error) {
	accumulator := scoreAccumulator{totalCases: len(document.Cases), terminationCounts: map[model.TerminationCategory]int{}}
	for _, adjudication := range document.Cases {
		if adjudication.ExecutionState == model.EvalIncomplete {
			accumulator.addIncompleteCase(adjudication)
			continue
		}
		if err := accumulator.addCompletedCase(adjudication); err != nil {
			return model.EvalScore{}, fmt.Errorf("case %q: %w", adjudication.CaseID, err)
		}
	}
	return accumulator.score(), nil
}

type scoreAccumulator struct {
	totalCases, incompleteCases, matchedExpected, missedExpected, uncertainExpected int
	reportedTotal, validReported, uncertainReported                                 int
	cleanCases, accurateCleanCases, cleanReported, cleanFalsePositives              int
	terminationCounts                                                               map[model.TerminationCategory]int
}

func (score *scoreAccumulator) addIncompleteCase(adjudication model.EvalCaseAdjudication) {
	score.incompleteCases++
	if adjudication.Termination != nil {
		score.terminationCounts[adjudication.Termination.Category]++
	}
}

func (score *scoreAccumulator) addCompletedCase(adjudication model.EvalCaseAdjudication) error {
	if err := validateCaseDispositions(adjudication); err != nil {
		return err
	}
	isClean := len(adjudication.ExpectedFindings) == 0
	score.addCleanCase(adjudication, isClean)
	for _, expected := range adjudication.ExpectedFindings {
		score.addExpected(expected)
	}
	for _, reported := range adjudication.ReportedFindings {
		score.addReported(reported, isClean)
	}
	return nil
}

func (score *scoreAccumulator) addCleanCase(adjudication model.EvalCaseAdjudication, clean bool) {
	if !clean {
		return
	}
	score.cleanCases++
	if len(adjudication.ReportedFindings) == 0 {
		score.accurateCleanCases++
	}
}

func (score *scoreAccumulator) addExpected(expected model.ExpectedFindingAdjudication) {
	switch expected.Disposition {
	case model.ExpectedMatched:
		score.matchedExpected++
	case model.ExpectedMissed:
		score.missedExpected++
	case model.ExpectedUncertain:
		score.uncertainExpected++
	}
}

func (score *scoreAccumulator) addReported(reported model.ReportedFindingAdjudication, clean bool) {
	if reported.Disposition == model.ReportedUncertain {
		score.uncertainReported++
		return
	}
	score.reportedTotal++
	if reported.Disposition == model.ReportedMatchedExpected || reported.Disposition == model.ReportedNovelValid {
		score.validReported++
	}
	if clean {
		score.cleanReported++
		if reported.Disposition == model.ReportedFalsePositive {
			score.cleanFalsePositives++
		}
	}
}

func (score scoreAccumulator) score() model.EvalScore {
	return model.EvalScore{
		DefectRecall: ratio(score.matchedExpected, score.matchedExpected+score.missedExpected), FindingPrecision: ratio(score.validReported, score.reportedTotal),
		CleanCaseAccuracy: ratio(score.accurateCleanCases, score.cleanCases), CleanFalsePositiveRate: ratio(score.cleanFalsePositives, score.cleanReported),
		CompletionRate: ratio(score.totalCases-score.incompleteCases, score.totalCases), UncertainExpected: score.uncertainExpected,
		UncertainReported: score.uncertainReported, IncompleteCases: score.incompleteCases, TerminationCounts: score.terminationCounts,
	}
}

func ratio(numerator, denominator int) model.RatioMetric {
	metric := model.RatioMetric{Numerator: numerator, Denominator: denominator}
	if denominator > 0 {
		value := float64(numerator) / float64(denominator)
		metric.Value = &value
	}
	return metric
}
