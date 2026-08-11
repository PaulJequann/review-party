package store

import "reviewparty/internal/model"

type ReviewID = model.ReviewID
type ReviewRecord = model.ReviewRecord
type SubjectFacts = model.SubjectFacts
type ReviewSubject = model.ReviewSubject

const LegacyReviewRecordSchemaVersion = model.LegacyReviewRecordSchemaVersion
const CurrentReviewRecordSchemaVersion = model.CurrentReviewRecordSchemaVersion

const currentReviewRecordSchemaVersion = model.CurrentReviewRecordSchemaVersion
