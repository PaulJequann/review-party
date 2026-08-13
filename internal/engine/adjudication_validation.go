package engine

import (
	"errors"
	"fmt"
	"reflect"

	"reviewparty/internal/model"
)

func validateAdjudicationShape(canonical, submitted model.AdjudicationDocument) error {
	if canonical.SuiteRunID != submitted.SuiteRunID {
		return errors.New("adjudication suite does not match the Eval Suite Run")
	}
	if len(canonical.Cases) != len(submitted.Cases) {
		return errors.New("adjudication case count does not match the Eval Suite Run")
	}
	for index := range canonical.Cases {
		if err := validateCaseShape(canonical.Cases[index], submitted.Cases[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateCaseShape(canonical, submitted model.EvalCaseAdjudication) error {
	if canonical.EvalRunID != submitted.EvalRunID {
		return fmt.Errorf("adjudication case %q changed Eval Run identity", submitted.CaseID)
	}
	if canonical.CaseID != submitted.CaseID {
		return fmt.Errorf("adjudication case %q changed case identity", submitted.CaseID)
	}
	if canonical.ExecutionState != submitted.ExecutionState {
		return fmt.Errorf("adjudication case %q changed execution state", submitted.CaseID)
	}
	if !reflect.DeepEqual(canonical.Termination, submitted.Termination) {
		return fmt.Errorf("adjudication case %q changed termination evidence", submitted.CaseID)
	}
	if len(canonical.ExpectedFindings) != len(submitted.ExpectedFindings) {
		return fmt.Errorf("adjudication case %q changed expected Finding membership", submitted.CaseID)
	}
	if len(canonical.ReportedFindings) != len(submitted.ReportedFindings) {
		return fmt.Errorf("adjudication case %q changed reported Finding membership", submitted.CaseID)
	}
	if err := validateExpectedShape(canonical, submitted); err != nil {
		return err
	}
	return validateReportedShape(canonical, submitted)
}

func validateExpectedShape(canonical, submitted model.EvalCaseAdjudication) error {
	for index := range canonical.ExpectedFindings {
		if !reflect.DeepEqual(canonical.ExpectedFindings[index].Finding, submitted.ExpectedFindings[index].Finding) {
			return fmt.Errorf("adjudication case %q changed expected Finding identity", submitted.CaseID)
		}
	}
	return nil
}

func validateReportedShape(canonical, submitted model.EvalCaseAdjudication) error {
	for index := range canonical.ReportedFindings {
		if !reflect.DeepEqual(canonical.ReportedFindings[index].Finding, submitted.ReportedFindings[index].Finding) {
			return fmt.Errorf("adjudication case %q changed reported Finding identity", submitted.CaseID)
		}
	}
	return nil
}

func validateCaseDispositions(adjudication model.EvalCaseAdjudication) error {
	expectedMatches, err := collectMatches(adjudication.ExpectedFindings, resolveExpectedMatch)
	if err != nil {
		return err
	}
	reportedMatches, err := collectMatches(adjudication.ReportedFindings, resolveReportedMatch)
	if err != nil {
		return err
	}
	return validateMatchedPairs(expectedMatches, reportedMatches)
}

func collectMatches[T any, K comparable, V any](findings []T, resolve func(T) (K, V, bool, error)) (map[K]V, error) {
	matches := map[K]V{}
	for _, finding := range findings {
		key, value, matched, err := resolve(finding)
		if err != nil {
			return nil, err
		}
		if matched {
			matches[key] = value
		}
	}
	return matches, nil
}

func resolveExpectedMatch(expected model.ExpectedFindingAdjudication) (string, int, bool, error) {
	ordinal, matched, err := validateExpectedDisposition(expected)
	return expected.Finding.ID, ordinal, matched, err
}

func resolveReportedMatch(reported model.ReportedFindingAdjudication) (int, string, bool, error) {
	expectedID, matched, err := validateReportedDisposition(reported)
	return reported.Finding.Ordinal, expectedID, matched, err
}

func validateExpectedDisposition(expected model.ExpectedFindingAdjudication) (int, bool, error) {
	if expected.Disposition == model.ExpectedMatched {
		if expected.ReportedOrdinal < 1 {
			return 0, false, fmt.Errorf("matched expected Finding %q requires a reported ordinal", expected.Finding.ID)
		}
		return expected.ReportedOrdinal, true, nil
	}
	if expected.Disposition != model.ExpectedMissed && expected.Disposition != model.ExpectedUncertain {
		return 0, false, fmt.Errorf("expected Finding %q has no valid disposition", expected.Finding.ID)
	}
	if expected.ReportedOrdinal != 0 {
		return 0, false, fmt.Errorf("unmatched expected Finding %q cannot reference a reported Finding", expected.Finding.ID)
	}
	return 0, false, nil
}

func validateReportedDisposition(reported model.ReportedFindingAdjudication) (string, bool, error) {
	if reported.Disposition == model.ReportedMatchedExpected {
		if reported.ExpectedFindingID == "" {
			return "", false, fmt.Errorf("matched reported Finding %d requires an expected Finding id", reported.Finding.Ordinal)
		}
		return reported.ExpectedFindingID, true, nil
	}
	valid := reported.Disposition == model.ReportedNovelValid || reported.Disposition == model.ReportedFalsePositive || reported.Disposition == model.ReportedUncertain
	if !valid {
		return "", false, fmt.Errorf("reported Finding %d has no valid disposition", reported.Finding.Ordinal)
	}
	if reported.ExpectedFindingID != "" {
		return "", false, fmt.Errorf("unmatched reported Finding %d cannot reference an expected Finding", reported.Finding.Ordinal)
	}
	return "", false, nil
}

func validateMatchedPairs(expected map[string]int, reported map[int]string) error {
	if len(expected) != len(reported) {
		return errors.New("matched Finding mappings disagree")
	}
	for expectedID, ordinal := range expected {
		if reported[ordinal] != expectedID {
			return errors.New("matched Finding mappings disagree")
		}
	}
	return nil
}
