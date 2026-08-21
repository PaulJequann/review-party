package engine

import (
	"context"
	"errors"
	"fmt"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const adjudicationSchemaVersion = 1

func (conductor *Conductor) ExportAdjudication(ctx context.Context, suiteRunID model.EvalSuiteRunID) (model.AdjudicationDocument, error) {
	suiteRun, err := conductor.InspectEvalSuiteRun(ctx, suiteRunID)
	if err != nil {
		return model.AdjudicationDocument{}, err
	}
	document := model.AdjudicationDocument{SchemaVersion: adjudicationSchemaVersion, SuiteRunID: suiteRunID, Cases: make([]model.EvalCaseAdjudication, 0, len(suiteRun.EvalRunIDs))}
	for _, evalRunID := range suiteRun.EvalRunIDs {
		caseAdjudication, err := conductor.exportCaseAdjudication(ctx, evalRunID)
		if err != nil {
			return model.AdjudicationDocument{}, err
		}
		document.Cases = append(document.Cases, caseAdjudication)
	}
	return document, nil
}

func (conductor *Conductor) exportCaseAdjudication(ctx context.Context, evalRunID model.EvalRunID) (model.EvalCaseAdjudication, error) {
	evalRun, err := conductor.InspectEvalRun(ctx, evalRunID)
	if err != nil {
		return model.EvalCaseAdjudication{}, err
	}
	if evalRun.ReviewID == "" {
		return model.EvalCaseAdjudication{}, fmt.Errorf("Eval Run %q is %s and cannot be adjudicated before an ordinary Review exists", evalRun.ID, evalRun.ExecutionState)
	}
	review, err := conductor.Inspect(ctx, evalRun.ReviewID)
	if err != nil {
		return model.EvalCaseAdjudication{}, err
	}
	result := model.EvalCaseAdjudication{EvalRunID: evalRun.ID, CaseID: evalRun.Case.ID, ExecutionState: evalRun.ExecutionState, Termination: review.Termination, ExpectedFindings: make([]model.ExpectedFindingAdjudication, 0, len(evalRun.Case.ExpectedFindings)), ReportedFindings: []model.ReportedFindingAdjudication{}}
	for _, finding := range evalRun.Case.ExpectedFindings {
		result.ExpectedFindings = append(result.ExpectedFindings, model.ExpectedFindingAdjudication{Finding: finding})
	}
	if review.Result != nil {
		for _, finding := range review.Result.Findings {
			result.ReportedFindings = append(result.ReportedFindings, model.ReportedFindingAdjudication{Finding: finding})
		}
	}
	return result, nil
}

func (conductor *Conductor) PublishAdjudication(ctx context.Context, document model.AdjudicationDocument) (model.AdjudicationRevision, error) {
	if document.SchemaVersion != adjudicationSchemaVersion {
		return model.AdjudicationRevision{}, fmt.Errorf("unsupported adjudication schema version %d", document.SchemaVersion)
	}
	canonical, err := conductor.ExportAdjudication(ctx, document.SuiteRunID)
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	if err := validateAdjudicationShape(canonical, document); err != nil {
		return model.AdjudicationRevision{}, err
	}
	score, err := EvaluateAdjudication(document)
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	ledger, ok := conductor.store.(store.AdjudicationStore)
	if !ok {
		return model.AdjudicationRevision{}, errors.New("adjudication requires the SQLite ledger")
	}
	id, err := newDomainID("ar", conductor.now().UTC())
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	revision := model.AdjudicationRevision{ID: model.AdjudicationRevisionID(id), SuiteRunID: document.SuiteRunID, Document: document, Score: score, CreatedAt: conductor.now().UTC()}
	return ledger.PublishAdjudication(revision)
}

func (conductor *Conductor) InspectAdjudication(_ context.Context, id model.AdjudicationRevisionID) (model.AdjudicationRevision, error) {
	ledger, ok := conductor.store.(store.AdjudicationStore)
	if !ok {
		return model.AdjudicationRevision{}, errors.New("adjudication inspection requires the SQLite ledger")
	}
	return ledger.LoadAdjudication(id)
}
