package engine

import "reviewparty/internal/result"

// ResultContract aliases — keep the original lower-case variable name used
// by engine code (canonicalReviewResultContract) but point to the exported
// result package variable. Also alias MaxResultSize.
var canonicalReviewResultContract = result.CanonicalReviewResultContract

// MaxResultSize is used by conductor's boundedAttemptOutput.
const maxResultSize = result.MaxResultSize

// parseReviewResult is used by adapters_test and result tests.
// Keep an alias for engine's own tests (adapters_test) that call it unqualified.
// The function is private in result (parseReviewResult) but we can expose a wrapper.
// Result package keeps it private; we provide an exported wrapper in result if needed.
// For now, engine's adapters_test calls parseReviewResult directly — that test
// should be moved to result or use the exported ResultContract.Parse instead.
// We provide a local alias that delegates to result's exported API for the
// engine's internal use.
var parseReviewResult = func(text string) (ReviewResult, error) {
	return result.CanonicalReviewResultContract.Parse(text)
}
