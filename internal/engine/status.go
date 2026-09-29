package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

var ErrUnsupportedStatusID = errors.New("want a Review Bundle id (rb_…) or a Review id (rp_…)")

const LifecycleUnreadable model.Lifecycle = "unreadable"

func (conductor *Conductor) Status(ctx context.Context, id string) (model.ReviewStatus, error) {
	switch {
	case strings.HasPrefix(id, "rb_"):
		return conductor.bundleStatus(ctx, model.ReviewBundleID(id))
	case strings.HasPrefix(id, "rp_"):
		record, err := conductor.Inspect(ctx, model.ReviewID(id))
		if err != nil {
			return model.ReviewStatus{}, err
		}
		return reviewStatus(record), nil
	default:
		return model.ReviewStatus{}, fmt.Errorf("%w; got %q", ErrUnsupportedStatusID, id)
	}
}

func (conductor *Conductor) InFlight(ctx context.Context, repository string) ([]model.ReviewStatus, error) {
	ledger, ok := conductor.store.(interface {
		InFlight(string) (store.InFlight, error)
	})
	if !ok {
		return nil, errors.New("review status requires the SQLite ledger")
	}
	ids, err := ledger.InFlight(repository)
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return nil, InitializationRequiredError{Repository: repository}
	}
	if err != nil {
		return nil, err
	}
	runIDs := make([]string, 0, len(ids.Bundles)+len(ids.Reviews))
	for _, id := range ids.Bundles {
		runIDs = append(runIDs, string(id))
	}
	for _, id := range ids.Reviews {
		runIDs = append(runIDs, string(id))
	}
	statuses := make([]model.ReviewStatus, 0, len(runIDs))
	for _, id := range runIDs {
		status, err := conductor.Status(ctx, id)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool { return statuses[i].CreatedAt.After(statuses[j].CreatedAt) })
	return statuses, nil
}

func (conductor *Conductor) bundleStatus(ctx context.Context, id model.ReviewBundleID) (model.ReviewStatus, error) {
	bundle, err := conductor.InspectBundle(ctx, id)
	if err != nil {
		return model.ReviewStatus{}, err
	}
	status := model.ReviewStatus{
		ID: string(bundle.ID), Kind: model.ReviewStatusBundle, Lifecycle: bundle.Lifecycle,
		Repository: bundle.Repository, Termination: bundle.Termination,
		CreatedAt: bundle.CreatedAt, UpdatedAt: bundle.UpdatedAt,
		Reviews: make([]model.ReviewStatusMember, 0, len(bundle.Members)),
	}
	for _, member := range bundle.Members {
		record, err := conductor.Inspect(ctx, member.ReviewID)
		if err != nil {
			status.Reviews = append(status.Reviews, model.ReviewStatusMember{
				ReviewID: member.ReviewID, Scope: member.Scope, Profile: member.Profile,
				Lifecycle: LifecycleUnreadable, ReadError: err.Error(),
			})
			continue
		}
		status.Reviews = append(status.Reviews, reviewStatusMember(record, member.Scope))
	}
	return status, nil
}

func reviewStatus(record model.ReviewRecord) model.ReviewStatus {
	return model.ReviewStatus{
		ID: string(record.ID), Kind: model.ReviewStatusReview, Lifecycle: record.Lifecycle,
		Repository: record.Subject.Repository, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		Reviews: []model.ReviewStatusMember{reviewStatusMember(record, "")},
	}
}

func reviewStatusMember(record model.ReviewRecord, scope string) model.ReviewStatusMember {
	member := model.ReviewStatusMember{
		ReviewID: record.ID, Scope: scope, Profile: record.ProfileRevision.Name,
		Reviewer: record.ProfileRevision.ReviewerID, Model: record.ProfileRevision.Model,
		Lifecycle: record.Lifecycle, Attempts: record.AttemptCount(), Termination: record.Termination,
		UpdatedAt: record.UpdatedAt,
	}
	if record.Result != nil {
		member.Status = string(record.Result.Status)
		member.FindingCount = record.Result.FindingCount()
	}
	if record.Lifecycle.Terminal() && record.Timings != nil {
		member.DurationMS = record.Timings.TotalMS
	}
	return member
}
