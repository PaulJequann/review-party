package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"reviewparty/internal/model"
)

const comparisonSchemaVersion = 1

var ErrIncompatibleComparison = errors.New("evaluation comparison has incompatible case evidence")

type comparisonCase struct {
	ID           string
	Digest       string
	Adjudication model.EvalCaseAdjudication
	RuntimeMS    int64
}

type comparisonSide struct {
	Revision model.AdjudicationRevision
	Suite    model.EvalSuiteRun
	Cases    map[string]comparisonCase
	Identity model.ComparisonIdentity
}

type comparisonProvenance struct {
	Identity                      model.ComparisonIdentity
	ProfileDigest, ReviewerDigest string
	RuntimeDigest                 string
}

func (conductor *Conductor) CompareAdjudications(ctx context.Context, baselineID, candidateID model.AdjudicationRevisionID) (model.EvalComparison, error) {
	baselineRevision, err := conductor.InspectAdjudication(ctx, baselineID)
	if err != nil {
		return model.EvalComparison{}, fmt.Errorf("load baseline adjudication %q: %w", baselineID, err)
	}
	candidateRevision, err := conductor.InspectAdjudication(ctx, candidateID)
	if err != nil {
		return model.EvalComparison{}, fmt.Errorf("load candidate adjudication %q: %w", candidateID, err)
	}
	baseline, err := conductor.loadComparisonSide(ctx, baselineRevision)
	if err != nil {
		return model.EvalComparison{}, fmt.Errorf("prepare baseline comparison: %w", err)
	}
	candidate, err := conductor.loadComparisonSide(ctx, candidateRevision)
	if err != nil {
		return model.EvalComparison{}, fmt.Errorf("prepare candidate comparison: %w", err)
	}
	return CompareEvalExperiments(baseline, candidate)
}

func (conductor *Conductor) loadComparisonSide(ctx context.Context, revision model.AdjudicationRevision) (comparisonSide, error) {
	suite, err := conductor.InspectEvalSuiteRun(ctx, revision.SuiteRunID)
	if err != nil {
		return comparisonSide{}, err
	}
	byEvalRun := make(map[model.EvalRunID]model.EvalCaseAdjudication, len(revision.Document.Cases))
	for _, adjudication := range revision.Document.Cases {
		byEvalRun[adjudication.EvalRunID] = adjudication
	}
	cases := make(map[string]comparisonCase, len(suite.EvalRunIDs))
	var provenance comparisonProvenance
	for index, evalRunID := range suite.EvalRunIDs {
		comparison, err := conductor.loadComparisonSideCase(ctx, comparisonCaseInput{Suite: suite, EvalRunID: evalRunID, Adjudications: byEvalRun, Previous: provenance, First: index == 0})
		if err != nil {
			return comparisonSide{}, err
		}
		provenance = comparison.Provenance
		if _, exists := cases[comparison.Case.ID]; exists {
			return comparisonSide{}, fmt.Errorf("duplicate comparison case %q", comparison.Case.ID)
		}
		cases[comparison.Case.ID] = comparison.Case
	}
	return comparisonSide{Revision: revision, Suite: suite, Cases: cases, Identity: provenance.Identity}, nil
}

type loadedComparisonCase struct {
	Case       comparisonCase
	Provenance comparisonProvenance
}

type comparisonCaseInput struct {
	Suite         model.EvalSuiteRun
	EvalRunID     model.EvalRunID
	Adjudications map[model.EvalRunID]model.EvalCaseAdjudication
	Previous      comparisonProvenance
	First         bool
}

func (conductor *Conductor) loadComparisonSideCase(ctx context.Context, input comparisonCaseInput) (loadedComparisonCase, error) {
	evalRun, err := conductor.InspectEvalRun(ctx, input.EvalRunID)
	if err != nil {
		return loadedComparisonCase{}, err
	}
	adjudication, ok := input.Adjudications[input.EvalRunID]
	if !ok {
		return loadedComparisonCase{}, fmt.Errorf("adjudication is missing Eval Run %q", input.EvalRunID)
	}
	if adjudication.CaseID != evalRun.Case.ID {
		return loadedComparisonCase{}, fmt.Errorf("adjudication case %q does not match Eval Run case %q", adjudication.CaseID, evalRun.Case.ID)
	}
	return conductor.loadComparisonCase(ctx, input, evalRun, adjudication)
}

func (conductor *Conductor) loadComparisonCase(ctx context.Context, input comparisonCaseInput, evalRun model.EvalRun, adjudication model.EvalCaseAdjudication) (loadedComparisonCase, error) {
	review, err := conductor.Inspect(ctx, evalRun.ReviewID)
	if err != nil {
		return loadedComparisonCase{}, err
	}
	provenance, err := comparisonReviewProvenance(input.Suite, review)
	if err != nil {
		return loadedComparisonCase{}, err
	}
	if !input.First && provenance != input.Previous {
		return loadedComparisonCase{}, errors.New("an experiment has inconsistent Profile, Reviewer, or runtime provenance across cases")
	}
	runtimeMS := int64(0)
	if review.Timings != nil {
		runtimeMS = review.Timings.TotalMS
	}
	return loadedComparisonCase{
		Case:       comparisonCase{ID: evalRun.Case.ID, Digest: evalRun.Case.Digest, Adjudication: adjudication, RuntimeMS: runtimeMS},
		Provenance: provenance,
	}, nil
}

func comparisonReviewProvenance(suite model.EvalSuiteRun, review model.ReviewRecord) (comparisonProvenance, error) {
	profileBytes, err := json.Marshal(review.ProfileRevision)
	if err != nil {
		return comparisonProvenance{}, err
	}
	reviewerBytes, err := json.Marshal(review.ProfileRevision.Reviewer)
	if err != nil {
		return comparisonProvenance{}, err
	}
	runtimeBytes, err := json.Marshal(review.Runtime)
	if err != nil {
		return comparisonProvenance{}, err
	}
	identity := model.ComparisonIdentity{
		Suite: suite.Suite, SuiteRevision: suite.SuiteRevision, SuiteDigest: suite.SuiteDigest,
		Experiment: suite.Experiment, ProfileRevisionDigest: digestBytes(profileBytes), Reviewer: review.ProfileRevision.Reviewer,
	}
	if review.Runtime != nil {
		identity.Runtime = *review.Runtime
	}
	return comparisonProvenance{Identity: identity, ProfileDigest: digestBytes(profileBytes), ReviewerDigest: digestBytes(reviewerBytes), RuntimeDigest: digestBytes(runtimeBytes)}, nil
}

func CompareEvalExperiments(baseline, candidate comparisonSide) (model.EvalComparison, error) {
	coverage := compareCoverage(baseline.Cases, candidate.Cases)
	if coverage.ComparedCases == 0 {
		return model.EvalComparison{}, fmt.Errorf("%w: no shared case revisions", ErrIncompatibleComparison)
	}
	baselineDocument := subsetDocument(baseline.Revision.Document, baseline.Cases, coverage.ComparedCaseIDs)
	candidateDocument := subsetDocument(candidate.Revision.Document, candidate.Cases, coverage.ComparedCaseIDs)
	baselineScore, err := EvaluateAdjudication(baselineDocument)
	if err != nil {
		return model.EvalComparison{}, fmt.Errorf("evaluate baseline comparison subset: %w", err)
	}
	candidateScore, err := EvaluateAdjudication(candidateDocument)
	if err != nil {
		return model.EvalComparison{}, fmt.Errorf("evaluate candidate comparison subset: %w", err)
	}
	baselineRuntime := comparisonRuntime(baseline.Cases, coverage.ComparedCaseIDs)
	candidateRuntime := comparisonRuntime(candidate.Cases, coverage.ComparedCaseIDs)
	return model.EvalComparison{
		SchemaVersion: comparisonSchemaVersion, BaselineAdjudication: baseline.Revision.ID, CandidateAdjudication: candidate.Revision.ID,
		BaselineIdentity: baseline.Identity, CandidateIdentity: candidate.Identity, Coverage: coverage,
		DefectRecall: comparisonMetric(baselineScore.DefectRecall, candidateScore.DefectRecall), FindingPrecision: comparisonMetric(baselineScore.FindingPrecision, candidateScore.FindingPrecision),
		CleanCaseAccuracy: comparisonMetric(baselineScore.CleanCaseAccuracy, candidateScore.CleanCaseAccuracy), CleanFalsePositiveRate: comparisonMetric(baselineScore.CleanFalsePositiveRate, candidateScore.CleanFalsePositiveRate), CompletionRate: comparisonMetric(baselineScore.CompletionRate, candidateScore.CompletionRate),
		BaselineTerminations: baselineScore.TerminationCounts, CandidateTerminations: candidateScore.TerminationCounts,
		BaselineRuntime: baselineRuntime, CandidateRuntime: candidateRuntime, RuntimeDeltaMS: candidateRuntime.TotalMS - baselineRuntime.TotalMS,
	}, nil
}

func compareCoverage(baseline, candidate map[string]comparisonCase) model.ComparisonCoverage {
	coverage := model.ComparisonCoverage{BaselineCases: len(baseline), CandidateCases: len(candidate)}
	for id, baseCase := range baseline {
		coverage = classifyBaselineCase(coverage, id, baseCase, candidate)
	}
	for id := range candidate {
		coverage = classifyCandidateOnlyCase(coverage, id, baseline)
	}
	sort.Strings(coverage.ComparedCaseIDs)
	sort.Strings(coverage.OmittedBaselineIDs)
	sort.Strings(coverage.OmittedCandidateIDs)
	sort.Strings(coverage.MismatchedCaseIDs)
	coverage.ComparedCases = len(coverage.ComparedCaseIDs)
	return coverage
}

func classifyBaselineCase(coverage model.ComparisonCoverage, id string, baseline comparisonCase, candidate map[string]comparisonCase) model.ComparisonCoverage {
	candidateCase, exists := candidate[id]
	if !exists {
		coverage.OmittedBaselineIDs = append(coverage.OmittedBaselineIDs, id)
		return coverage
	}
	if baseline.Digest != candidateCase.Digest {
		coverage.MismatchedCaseIDs = append(coverage.MismatchedCaseIDs, id)
		return coverage
	}
	coverage.ComparedCaseIDs = append(coverage.ComparedCaseIDs, id)
	return coverage
}

func classifyCandidateOnlyCase(coverage model.ComparisonCoverage, id string, baseline map[string]comparisonCase) model.ComparisonCoverage {
	if _, exists := baseline[id]; !exists {
		coverage.OmittedCandidateIDs = append(coverage.OmittedCandidateIDs, id)
	}
	return coverage
}

func subsetDocument(document model.AdjudicationDocument, cases map[string]comparisonCase, ids []string) model.AdjudicationDocument {
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	result := model.AdjudicationDocument{SchemaVersion: document.SchemaVersion, SuiteRunID: document.SuiteRunID, Cases: make([]model.EvalCaseAdjudication, 0, len(ids))}
	for _, adjudication := range document.Cases {
		if selected[adjudication.CaseID] {
			result.Cases = append(result.Cases, cases[adjudication.CaseID].Adjudication)
		}
	}
	return result
}

func comparisonRuntime(cases map[string]comparisonCase, ids []string) model.ComparisonRuntime {
	result := model.ComparisonRuntime{ComparedCases: len(ids)}
	for _, id := range ids {
		result.TotalMS += cases[id].RuntimeMS
	}
	if result.ComparedCases > 0 {
		result.AverageMS = result.TotalMS / int64(result.ComparedCases)
	}
	return result
}

func comparisonMetric(baseline, candidate model.RatioMetric) model.ComparisonMetric {
	metric := model.ComparisonMetric{Baseline: baseline, Candidate: candidate}
	if baseline.Value != nil && candidate.Value != nil {
		delta := *candidate.Value - *baseline.Value
		metric.Delta = &delta
	}
	return metric
}

func digestBytes(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
