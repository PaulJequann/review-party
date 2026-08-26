package engine

import (
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"reviewparty/internal/result"
	"reviewparty/internal/subject"
	"time"
)

var ErrWorkingChangesReplayUnsupported = errors.New("working-changes Reviews cannot be replayed; replay requires a committed-range Subject")

func (conductor *Conductor) prepareReplay(source model.ReviewRecord, selection model.ReplaySelection) (preparedReview, error) {
	timings := model.ReviewTimings{}
	subjectStarted := conductor.now().UTC()
	subject, err := replaySubject(source.Subject)
	timings.SubjectResolutionMS = elapsedMilliseconds(subjectStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, err
	}
	profileStarted := conductor.now().UTC()
	profile, err := conductor.replayProfile(source, selection)
	timings.ProfileCompilationMS = elapsedMilliseconds(profileStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, err
	}
	deadline, err := time.ParseDuration(profile.revision.ExecutionDeadline)
	if err != nil || deadline <= 0 {
		return preparedReview{}, fmt.Errorf("replay Profile has invalid execution deadline %q", profile.revision.ExecutionDeadline)
	}
	return preparedReview{subject: subject, profile: profile, timings: timings, deadline: deadline}, nil
}

func replaySubject(recorded model.ReviewSubject) (model.ReviewSubject, error) {
	if recorded.Kind != model.SubjectCommittedRange {
		return model.ReviewSubject{}, ErrWorkingChangesReplayUnsupported
	}
	resolved, err := subject.ResolveSubject(recorded.Repository, model.CommittedRange(recorded.BaseObject, recorded.HeadObject))
	if err != nil {
		return model.ReviewSubject{}, fmt.Errorf("reconstruct replay Subject: %w", err)
	}
	if resolved.Identity != recorded.Identity {
		return model.ReviewSubject{}, errors.New("reconstructed replay Subject does not match the recorded identity")
	}
	return recorded, nil
}

func (conductor *Conductor) replayProfile(source model.ReviewRecord, selection model.ReplaySelection) (compiledProfile, error) {
	revision := source.ProfileRevision
	if err := validateReplayProfile(revision); err != nil {
		return compiledProfile{}, err
	}
	registration, err := conductor.resolveReplayReviewer(revision, selection)
	if err != nil {
		return compiledProfile{}, err
	}
	return replayCompiledProfile(revision, source.ProfileSnapshot, registration), nil
}

func validateReplayProfile(revision model.ProfileRevision) error {
	if revision.ResultContract != result.CanonicalReviewResultContract.Revision() {
		return fmt.Errorf("replay Profile uses unsupported result contract %q", revision.ResultContract)
	}
	if len(revision.Passes) != 1 || revision.AttemptLimit != 1 {
		return errors.New("replay Profile requires the supported single-Pass, single-Attempt contract")
	}
	return nil
}

func (conductor *Conductor) resolveReplayReviewer(revision model.ProfileRevision, selection model.ReplaySelection) (reviewerRegistration, error) {
	if replayHasExecutionOverrides(selection) {
		return reviewerRegistration{}, errors.New("Replay does not accept Reviewer, model, or effort overrides")
	}
	registration, err := conductor.reviewers.resolve(revision.ReviewerID)
	if err != nil {
		return reviewerRegistration{}, err
	}
	if err := validateRecordedTransport(registration, revision); err != nil {
		return reviewerRegistration{}, err
	}
	if err := validateReplayCapabilities(revision, revision.ReviewerID, registration); err != nil {
		return reviewerRegistration{}, err
	}
	return resolveReviewerSelection(registration, model.ProfileSelection{Reviewer: revision.ReviewerID, Model: revision.Model, Effort: revision.Effort})
}

func replayHasExecutionOverrides(selection model.ReplaySelection) bool {
	return selection.Reviewer != "" || selection.Model != "" || selection.Effort != ""
}

func validateReplayCapabilities(revision model.ProfileRevision, reviewerID string, registration reviewerRegistration) error {
	missing := missingCapabilities(revision.RequiredCapabilities, registration.capabilities)
	if len(missing) == 0 {
		return nil
	}
	return UnsupportedCapabilitiesError{Profile: revision.Name, Reviewer: reviewerID, Missing: missing}
}

func validateRecordedTransport(registration reviewerRegistration, revision model.ProfileRevision) error {
	if registration.candidate.Harness == revision.Reviewer.Harness && registration.candidate.Transport == revision.Reviewer.Transport {
		return nil
	}
	return fmt.Errorf("recorded reviewer transport %s/%s is unavailable", revision.Reviewer.Harness, revision.Reviewer.Transport)
}

func replayCompiledProfile(revision model.ProfileRevision, snapshot model.ProfileSnapshot, registration reviewerRegistration) compiledProfile {
	resolved := resolvedProfile{name: snapshot.Name, instructions: snapshot.Instructions, source: snapshot.Source, digest: snapshot.SourceDigest}
	return compiledProfile{
		revision: revision,
		snapshot: snapshot,
		reviewer: registration,
		buildPrompt: func(subject model.ReviewSubject) string {
			return renderReviewPrompt(resolved, subject)
		},
	}
}
