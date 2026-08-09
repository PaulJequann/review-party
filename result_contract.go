package reviewparty

import "fmt"

const (
	resultContractRevision = "canonical-v1"
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

type resultContract struct{}

var canonicalReviewResultContract resultContract

func (resultContract) revision() string { return resultContractRevision }

func (resultContract) instructions() string {
	return fmt.Sprintf(`Return exactly one block and no text before or after it.

For no actionable findings:
%s

For findings, return at most eight MEDIUM, HIGH, or CRITICAL items:
%s`, cleanReviewExample, findingsReviewExample)
}

func (resultContract) parse(assistantText string) (ReviewResult, error) {
	return parseReviewResult(assistantText)
}
