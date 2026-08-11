package result

import (
	"fmt"
	"reviewparty/internal/model"
)

const (
	resultContractRevision = "canonical-v2"
	cleanReviewExample     = `BEGIN_REVIEW
status: clean
summary: No actionable findings.
END_REVIEW`
	findingsReviewExample = `BEGIN_REVIEW
status: findings

1. HIGH | correctness | path/to/file.go:123
Failure: Concrete supported scenario that fails.
Evidence: Why the Subject permits the failure.
Fix: Smallest safe correction.
Test: Regression that fails before the correction.
END_REVIEW`
)

type ResultContract struct{}

var CanonicalReviewResultContract ResultContract

func (ResultContract) Revision() string { return resultContractRevision }

func (ResultContract) Instructions() string {
	return fmt.Sprintf(`Return exactly one block and no text before or after it.

For no actionable findings:
%s

For findings, return at most eight MEDIUM, HIGH, or CRITICAL items:
%s`, cleanReviewExample, findingsReviewExample)
}

func (ResultContract) Parse(assistantText string) (model.ReviewResult, error) {
	return parseReviewResult(assistantText)
}
