package subject

import "reviewparty/internal/model"

// Model type aliases for tests and internal helpers that still use unqualified names.
type SubjectFacts = model.SubjectFacts
type ReviewSubject = model.ReviewSubject
type SubjectReference = model.SubjectReference
type SubjectKind = model.SubjectKind

const SubjectWorkingChanges = model.SubjectWorkingChanges
const SubjectCommittedRange = model.SubjectCommittedRange
const SubjectCapturedChange = model.SubjectCapturedChange
