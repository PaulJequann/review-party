package result

import "reviewparty/internal/model"

type ReviewResult = model.ReviewResult
type Finding = model.Finding
type ResultStatus = model.ResultStatus
type ReviewSubject = model.ReviewSubject

const ResultClean = model.ResultClean
const ResultFindings = model.ResultFindings

// For tests that use AttemptInvalidResult etc. (model constant) but test is in result
// That test should be moved, but provide alias for now
const AttemptInvalidResult = model.AttemptInvalidResult

// Backward compat for private maxResultSize vs exported MaxResultSize
const maxResultSize = MaxResultSize
