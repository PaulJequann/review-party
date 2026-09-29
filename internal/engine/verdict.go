package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

// ParseVerdictReview reads the one review a verdict batch judges. A review
// bundle is refused: its members number their Findings independently.
func ParseVerdictReview(id string) (model.ReviewID, error) {
	switch {
	case strings.HasPrefix(id, "rb_") && validReviewID(model.ReviewID(id)):
		return "", fmt.Errorf("%q is a review bundle; name one of its member reviews (rp_...)", id)
	case strings.HasPrefix(id, "rp_") && validReviewID(model.ReviewID(id)):
		return model.ReviewID(id), nil
	}
	return "", fmt.Errorf("invalid review id %q; expected rp_...", id)
}

type VerdictRequest struct {
	Review     model.ReviewID
	Judgments  []model.Judgment
	RecordedBy string
}

func (conductor *Conductor) RecordVerdicts(_ context.Context, request VerdictRequest) (store.VerdictTally, error) {
	ledger, err := conductor.verdictStore()
	if err != nil {
		return store.VerdictTally{}, err
	}
	tally, err := ledger.RecordVerdicts(store.VerdictBatch{
		ReviewID: request.Review, Judgments: request.Judgments, RecordedBy: request.RecordedBy, RecordedAt: conductor.now(),
	})
	return tally, ledgerStateError(err)
}

func (conductor *Conductor) FindingVerdicts(_ context.Context, query store.VerdictQuery) ([]model.FindingVerdict, error) {
	ledger, err := conductor.verdictStore()
	if err != nil {
		return nil, err
	}
	verdicts, err := ledger.ListVerdicts(query)
	return verdicts, ledgerStateError(err)
}

func (conductor *Conductor) verdictStore() (store.VerdictStore, error) {
	ledger, ok := conductor.store.(store.VerdictStore)
	if !ok {
		return nil, errors.New("finding verdicts require the SQLite ledger")
	}
	return ledger, nil
}
