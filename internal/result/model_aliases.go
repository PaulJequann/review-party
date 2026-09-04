package result

import "reviewparty/internal/model"

type ReviewResult = model.ReviewResult
type Finding = model.Finding
type ResultStatus = model.ResultStatus
type ReviewSubject = model.ReviewSubject

const ResultClean = model.ResultClean
const ResultFindings = model.ResultFindings
const ResultFindingsPartial = model.ResultFindingsPartial

// AttemptInvalidResult keeps the model constant available to result-package tests.
const AttemptInvalidResult = model.AttemptInvalidResult
