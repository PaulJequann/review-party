package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

type MissTarget struct {
	review  model.ReviewID
	bundle  model.ReviewBundleID
	profile string
}

func ParseMissTarget(id, profile string) (MissTarget, error) {
	if !validReviewID(model.ReviewID(id)) {
		return MissTarget{}, fmt.Errorf("invalid miss target %q; expected a review id (rp_...) or a review bundle id (rb_...)", id)
	}
	switch {
	case strings.HasPrefix(id, "rp_") && profile != "":
		return MissTarget{}, fmt.Errorf("--profile narrows a review bundle; review %q already names one profile", id)
	case strings.HasPrefix(id, "rp_"):
		return MissTarget{review: model.ReviewID(id)}, nil
	case strings.HasPrefix(id, "rb_"):
		return MissTarget{bundle: model.ReviewBundleID(id), profile: profile}, nil
	}
	return MissTarget{}, fmt.Errorf("invalid miss target %q; expected a review id (rp_...) or a review bundle id (rb_...)", id)
}

func ParseMissID(id string) (model.MissID, error) {
	if !strings.HasPrefix(id, "ms_") || !validReviewID(model.ReviewID(id)) {
		return "", fmt.Errorf("invalid miss id %q; expected ms_...", id)
	}
	return model.MissID(id), nil
}

type MissRequest struct {
	Target MissTarget
	Report model.MissReport
}

type MissRemovalRequest struct {
	IDs       []model.MissID
	Reason    string
	RemovedBy string
}

func (conductor *Conductor) RecordMiss(_ context.Context, request MissRequest) ([]model.Miss, error) {
	ledger, err := conductor.missStore()
	if err != nil {
		return nil, err
	}
	reviews, err := conductor.missReviews(request.Target)
	if err != nil {
		return nil, missStateError(err)
	}
	now := conductor.now()
	records := make([]store.MissRecord, 0, len(reviews))
	for _, review := range reviews {
		id, err := newDomainID("ms", now)
		if err != nil {
			return nil, err
		}
		records = append(records, store.MissRecord{ID: model.MissID(id), ReviewID: review, Report: request.Report, RecordedAt: now})
	}
	misses, err := ledger.RecordMisses(records)
	return misses, missStateError(err)
}

func (conductor *Conductor) Misses(_ context.Context, query store.MissQuery) ([]model.Miss, error) {
	ledger, err := conductor.missStore()
	if err != nil {
		return nil, err
	}
	misses, err := ledger.ListMisses(query)
	return misses, missStateError(err)
}

func (conductor *Conductor) RemoveMisses(_ context.Context, request MissRemovalRequest) ([]store.MissRemovalOutcome, error) {
	ledger, err := conductor.missStore()
	if err != nil {
		return nil, err
	}
	removal := model.MissRemoval{Reason: request.Reason, RemovedBy: request.RemovedBy, RemovedAt: conductor.now()}
	outcomes, err := ledger.RemoveMisses(request.IDs, removal)
	return outcomes, missStateError(err)
}

func (conductor *Conductor) missStore() (store.MissStore, error) {
	ledger, ok := conductor.store.(store.MissStore)
	if !ok {
		return nil, errors.New("misses require the SQLite ledger")
	}
	return ledger, nil
}

func missStateError(err error) error {
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return InitializationRequiredError{Repository: "."}
	}
	return err
}

func (conductor *Conductor) missReviews(target MissTarget) ([]model.ReviewID, error) {
	if target.bundle == "" {
		return []model.ReviewID{target.review}, conductor.requireCompletedReview(target.review)
	}
	ledger, ok := conductor.store.(store.BundleStore)
	if !ok {
		return nil, errors.New("misses on a review bundle require the SQLite ledger")
	}
	bundle, err := ledger.LoadReviewBundle(target.bundle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no review bundle with id %q", target.bundle)
	}
	if err != nil {
		return nil, err
	}
	members, err := targetedBundleMembers(bundle, target.profile)
	if err != nil {
		return nil, err
	}
	return conductor.completedMemberReviews(bundle.ID, members)
}

func (conductor *Conductor) requireCompletedReview(id model.ReviewID) error {
	lifecycle, err := conductor.reviewLifecycle(id)
	if err != nil {
		return err
	}
	if lifecycle != model.LifecycleCompleted {
		return fmt.Errorf("review %q is %s; misses attach only to completed reviews", id, lifecycle)
	}
	return nil
}

func (conductor *Conductor) reviewLifecycle(id model.ReviewID) (model.Lifecycle, error) {
	record, err := conductor.store.Load(id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("no review with id %q", id)
	}
	return record.Lifecycle, err
}

func targetedBundleMembers(bundle model.ReviewBundle, profile string) ([]model.BundleMember, error) {
	if profile == "" {
		return bundle.Members, nil
	}
	scope, _ := configuration.ParseScopedReference(profile)
	var matched []model.BundleMember
	var labels, matchedLabels []string
	for _, member := range bundle.Members {
		labels = append(labels, memberLabel(member))
		reference := member.Profile
		if scope != "" {
			reference = memberLabel(member)
		}
		if reference == profile {
			matched = append(matched, member)
			matchedLabels = append(matchedLabels, memberLabel(member))
		}
	}
	switch len(matched) {
	case 0:
		return nil, fmt.Errorf("review bundle %q has no member with profile %q; members: %s", bundle.ID, profile, strings.Join(labels, ", "))
	case 1:
		return matched, nil
	}
	return nil, fmt.Errorf("review bundle %q: profile %q matches more than one member; choose one of: %s", bundle.ID, profile, strings.Join(matchedLabels, ", "))
}

func memberLabel(member model.BundleMember) string {
	return member.Scope + ":" + member.Profile
}

func (conductor *Conductor) completedMemberReviews(bundle model.ReviewBundleID, members []model.BundleMember) ([]model.ReviewID, error) {
	reviews := make([]model.ReviewID, 0, len(members))
	var unusable []string
	for _, member := range members {
		lifecycle, err := conductor.reviewLifecycle(member.ReviewID)
		if err != nil {
			return nil, err
		}
		if lifecycle != model.LifecycleCompleted {
			unusable = append(unusable, fmt.Sprintf("%s (%s %s)", memberLabel(member), member.ReviewID, lifecycle))
			continue
		}
		reviews = append(reviews, member.ReviewID)
	}
	if len(unusable) > 0 {
		return nil, fmt.Errorf("review bundle %q has members that cannot take a miss: %s; narrow with --profile or target completed rp_ review ids", bundle, strings.Join(unusable, "; "))
	}
	return reviews, nil
}
