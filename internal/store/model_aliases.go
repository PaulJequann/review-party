package store

import "reviewparty/internal/model"

type ReviewID = model.ReviewID
type ReviewRecord = model.ReviewRecord
type SubjectFacts = model.SubjectFacts
type ReviewSubject = model.ReviewSubject

const LegacyReviewRecordSchemaVersion = model.LegacyReviewRecordSchemaVersion
const CurrentReviewRecordSchemaVersion = model.CurrentReviewRecordSchemaVersion

// Backward compat for tests that call private names
type fileRecordStore = FileRecordStore

var newFileRecordStore = NewFileRecordStore

// Legacy constants used by tests (unqualified)
const legacyReviewRecordSchemaVersion = model.LegacyReviewRecordSchemaVersion
const currentReviewRecordSchemaVersion = model.CurrentReviewRecordSchemaVersion
