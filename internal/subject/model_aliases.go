package subject

import "reviewparty/internal/model"

// SubjectFacts is an alias for the model type used by subject-package tests and helpers.
type SubjectFacts = model.SubjectFacts
type ReviewSubject = model.ReviewSubject
type SubjectReference = model.SubjectReference
type SubjectKind = model.SubjectKind

const SubjectWorkingChanges = model.SubjectWorkingChanges
const SubjectCommittedRange = model.SubjectCommittedRange
const SubjectCapturedChange = model.SubjectCapturedChange
const SubjectUnreviewedDelta = model.SubjectUnreviewedDelta
