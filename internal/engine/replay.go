package engine

import (
	"errors"
	"fmt"
	"time"
)

var ErrWorkingChangesReplayUnsupported = errors.New("working-changes Reviews cannot be replayed; replay requires a committed-range Subject")

func (conductor *Conductor) prepareReplay(source ReviewRecord, selection ReplaySelection) (preparedReview, error) {
	timings := ReviewTimings{}
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

func replaySubject(recorded ReviewSubject) (ReviewSubject, error) {
	if recorded.Kind != SubjectCommittedRange {
		return ReviewSubject{}, ErrWorkingChangesReplayUnsupported
	}
	resolved, err := resolveSubject(recorded.Repository, CommittedRange(recorded.BaseObject, recorded.HeadObject))
	if err != nil {
		return ReviewSubject{}, fmt.Errorf("reconstruct replay Subject: %w", err)
	}
	if resolved.Identity != recorded.Identity {
		return ReviewSubject{}, errors.New("reconstructed replay Subject does not match the recorded identity")
	}
	return recorded, nil
}

func (conductor *Conductor) replayProfile(source ReviewRecord, selection ReplaySelection) (compiledProfile, error) {
	revision := source.ProfileRevision
	if err := validateReplayProfile(revision); err != nil {
		return compiledProfile{}, err
	}
	registration, overridden, err := conductor.resolveReplayReviewer(revision, selection)
	if err != nil {
		return compiledProfile{}, err
	}
	if overridden {
		revision = replayRevisionWithReviewer(revision, registration)
	}
	return replayCompiledProfile(revision, source.ProfileSnapshot, registration), nil
}

func validateReplayProfile(revision ProfileRevision) error {
	if revision.ResultContract != canonicalReviewResultContract.Revision() {
		return fmt.Errorf("replay Profile uses unsupported result contract %q", revision.ResultContract)
	}
	if len(revision.Passes) != 1 || revision.AttemptLimit != 1 {
		return errors.New("replay Profile requires the supported single-Pass, single-Attempt contract")
	}
	return nil
}

func (conductor *Conductor) resolveReplayReviewer(revision ProfileRevision, selection ReplaySelection) (reviewerRegistration, bool, error) {
	overridden := selection.Reviewer != "" || selection.Model != "" || selection.Effort != ""
	reviewerID := firstNonempty(selection.Reviewer, revision.ReviewerID)
	registration, err := conductor.reviewers.resolve(reviewerID)
	if err != nil {
		return reviewerRegistration{}, false, err
	}
	selection, err = completeReplaySelection(selection, revision, registration)
	if err != nil {
		return reviewerRegistration{}, false, err
	}
	if err := validateReplayCapabilities(revision, reviewerID, registration); err != nil {
		return reviewerRegistration{}, false, err
	}
	registration, err = resolveReviewerSelection(registration, ProfileSelection{Reviewer: reviewerID, Model: selection.Model, Effort: selection.Effort})
	if err != nil {
		return reviewerRegistration{}, false, err
	}
	return registration, overridden, nil
}

func completeReplaySelection(selection ReplaySelection, revision ProfileRevision, registration reviewerRegistration) (ReplaySelection, error) {
	if selection.Reviewer != "" {
		return selection, nil
	}
	if err := validateRecordedTransport(registration, revision); err != nil {
		return ReplaySelection{}, err
	}
	selection.Model = firstNonempty(selection.Model, revision.Model)
	selection.Effort = firstNonempty(selection.Effort, revision.Effort)
	return selection, nil
}

func validateReplayCapabilities(revision ProfileRevision, reviewerID string, registration reviewerRegistration) error {
	missing := missingCapabilities(revision.RequiredCapabilities, registration.capabilities)
	if len(missing) == 0 {
		return nil
	}
	return UnsupportedCapabilitiesError{Profile: revision.Name, Reviewer: reviewerID, Missing: missing}
}

func validateRecordedTransport(registration reviewerRegistration, revision ProfileRevision) error {
	if registration.candidate.Harness == revision.Reviewer.Harness && registration.candidate.Transport == revision.Reviewer.Transport {
		return nil
	}
	return fmt.Errorf("recorded reviewer transport %s/%s is unavailable", revision.Reviewer.Harness, revision.Reviewer.Transport)
}

func replayRevisionWithReviewer(revision ProfileRevision, registration reviewerRegistration) ProfileRevision {
	revision.ReviewerID = registration.candidate.ID
	revision.Model = registration.candidate.Model
	revision.Effort = registration.candidate.Effort
	revision.Reviewer = registration.candidate.provenance()
	revision.Revision = profileRevisionIdentity(revision)
	return revision
}

func replayCompiledProfile(revision ProfileRevision, snapshot ProfileSnapshot, registration reviewerRegistration) compiledProfile {
	resolved := resolvedProfile{name: snapshot.Name, instructions: snapshot.Instructions, source: snapshot.Source, digest: snapshot.SourceDigest}
	return compiledProfile{
		revision: revision,
		snapshot: snapshot,
		reviewer: registration,
		buildPrompt: func(subject ReviewSubject) string {
			return renderReviewPrompt(resolved, subject)
		},
	}
}
