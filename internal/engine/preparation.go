package engine

import (
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

// reviewPreparation owns the shared two-stage work between a raw Subject
// reference and the immutable value handed to Review execution. Callers keep
// selection and experiment policy outside this seam.
type reviewPreparation struct {
	repository       string
	reference        model.SubjectReference
	rootResolutionMS int64
}

func (conductor *Conductor) startReviewPreparation(repository string, reference model.SubjectReference) (reviewPreparation, error) {
	started := conductor.now().UTC()
	canonical, err := canonicalSubjectRepository(repository, reference)
	if err != nil {
		return reviewPreparation{}, err
	}
	return reviewPreparation{
		repository:       canonical,
		reference:        reference,
		rootResolutionMS: elapsedMilliseconds(started, conductor.now().UTC()),
	}, nil
}

func canonicalSubjectRepository(repository string, reference model.SubjectReference) (string, error) {
	if reference.Kind == model.SubjectCapturedChange {
		return repository, nil
	}
	return subject.ResolveRepositoryRoot(repository)
}

type preparedSubject struct {
	value               subject.Subject
	subjectResolutionMS int64
}

func (conductor *Conductor) completeReviewPreparation(preparation reviewPreparation) (preparedSubject, error) {
	started := conductor.now().UTC()
	resolved, err := subject.ResolveSubject(preparation.repository, preparation.reference)
	resolutionMS := preparation.rootResolutionMS + elapsedMilliseconds(started, conductor.now().UTC())
	if err != nil {
		return preparedSubject{}, err
	}
	return preparedSubject{value: resolved, subjectResolutionMS: resolutionMS}, nil
}

func (prepared preparedSubject) review(profile compiledProfile, timings model.ReviewTimings) preparedReview {
	timings.SubjectResolutionMS = prepared.subjectResolutionMS
	return preparedReview{subject: prepared.value, profile: profile, timings: timings, deadline: profile.deadline}
}
