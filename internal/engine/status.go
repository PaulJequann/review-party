package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

var ErrUnsupportedStatusID = errors.New("want a Review Bundle id (rb_…) or a Review id (rp_…)")

const LifecycleUnreadable model.Lifecycle = "unreadable"

// staleSlack covers the work around a deadline-bounded attempt that no
// deadline bounds: the availability check, checkout preparation, process
// teardown, and the final save.
const staleSlack = 2 * time.Minute

func (conductor *Conductor) Status(ctx context.Context, id string) (model.ReviewStatus, error) {
	switch {
	case strings.HasPrefix(id, "rb_"):
		return conductor.bundleStatus(ctx, model.ReviewBundleID(id))
	case strings.HasPrefix(id, "rp_"):
		return conductor.reviewStatus(ctx, model.ReviewID(id))
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
	var longest time.Duration
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
		longest = max(longest, executionDeadline(record))
	}
	markStale(&status, longest, conductor.now())
	return status, nil
}

// reviewStatus judges a pending Review through the Review Bundle that owns
// it: a queued member legitimately waits on the members ahead of it.
func (conductor *Conductor) reviewStatus(ctx context.Context, id model.ReviewID) (model.ReviewStatus, error) {
	record, err := conductor.Inspect(ctx, id)
	if err != nil {
		return model.ReviewStatus{}, err
	}
	status := model.ReviewStatus{
		ID: string(record.ID), Kind: model.ReviewStatusReview, Lifecycle: record.Lifecycle,
		Repository: record.Subject.Repository, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		Reviews: []model.ReviewStatusMember{reviewStatusMember(record, "")},
	}
	owner, err := conductor.owningBundle(record)
	if err != nil || owner == "" {
		markStale(&status, executionDeadline(record), conductor.now())
		return status, err
	}
	bundle, err := conductor.bundleStatus(ctx, owner)
	status.Stale = bundle.Stale
	return status, err
}

func (conductor *Conductor) owningBundle(record model.ReviewRecord) (model.ReviewBundleID, error) {
	ledger, ok := conductor.store.(interface {
		ReviewBundleOwning(model.ReviewID) (model.ReviewBundleID, error)
	})
	if !ok || record.Lifecycle != model.LifecyclePending {
		return "", nil
	}
	return ledger.ReviewBundleOwning(record.ID)
}

// markStale flags a non-terminal run whose newest ledger write is older than
// its longest execution deadline plus staleSlack.
func markStale(status *model.ReviewStatus, deadline time.Duration, now time.Time) {
	if status.Lifecycle.Terminal() {
		return
	}
	last := status.UpdatedAt
	for _, member := range status.Reviews {
		if member.UpdatedAt.After(last) {
			last = member.UpdatedAt
		}
	}
	quiet, limit := now.Sub(last), deadline+staleSlack
	if quiet > limit {
		status.Stale = &model.StaleRun{LastProgressAt: last, QuietMS: quiet.Milliseconds(), LimitMS: limit.Milliseconds()}
	}
}

// executionDeadline reads the deadline snapshotted with the Review; a
// deadline that does not parse adds nothing beyond staleSlack.
func executionDeadline(record model.ReviewRecord) time.Duration {
	deadline, err := time.ParseDuration(record.ProfileRevision.ExecutionDeadline)
	if err != nil {
		return 0
	}
	return deadline
}

func reviewStatusMember(record model.ReviewRecord, scope string) model.ReviewStatusMember {
	member := model.ReviewStatusMember{
		ReviewID: record.ID, Scope: scope, Profile: record.ProfileRevision.Name,
		Reviewer: record.ProfileRevision.ReviewerID, Model: record.ProfileRevision.Model,
		Lifecycle: record.Lifecycle, Attempts: record.AttemptCount(), Termination: record.Termination,
		UpdatedAt: record.UpdatedAt,
	}
	if record.Lifecycle == model.LifecycleRunning {
		// A running Review records its Attempt only once the Attempt ends.
		member.Attempts++
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
