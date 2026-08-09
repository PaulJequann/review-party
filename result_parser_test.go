package reviewparty

import (
	"errors"
	"strings"
	"testing"
)

func TestReviewAcceptsCRLFFindings(t *testing.T) {
	review := strings.ReplaceAll(findingsReview, "\n", "\r\n")
	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount != 1 {
		t.Fatalf("result = %#v, want one finding", result)
	}
}

func TestCanonicalResultExamplesSatisfyContract(t *testing.T) {
	profile, err := compileTestProfile(profileLibrary{}, "bugs", "grok", ReviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	prompt := profile.prompt(ReviewSubject{})
	for _, example := range []string{cleanReviewExample, findingsReviewExample} {
		if _, err := canonicalReviewResultContract.parse(example); err != nil {
			t.Fatalf("canonical example is invalid: %v\n%s", err, example)
		}
		if !strings.Contains(prompt, example) {
			t.Fatalf("Review Profile prompt omits canonical example:\n%s", example)
		}
	}
}

func TestInlineMarkerNamesDoNotReplaceTerminalReviewBlock(t *testing.T) {
	review := strings.Replace(findingsReview, "Fix: Update the caller with the state change.", "Fix: Preserve literal `BEGIN_REVIEW` and `END_REVIEW` examples.", 1)
	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount != 1 {
		t.Fatalf("result = %#v, want one finding", result)
	}
}

func TestMultipleReviewBlocksAreInvalid(t *testing.T) {
	_, err := parseReviewResult(findingsReview + "\n" + cleanReview)
	if err == nil || !strings.Contains(err.Error(), "multiple review blocks") {
		t.Fatalf("error = %v, want multiple review blocks", err)
	}
}

func TestFindingFieldsMustBelongToTheirFinding(t *testing.T) {
	review := `BEGIN_REVIEW
status: findings
Failure: decoy
Evidence: decoy
Fix: decoy
Test: decoy

1. HIGH | correctness | review.go:3
END_REVIEW`
	if _, err := parseReviewResult(review); err == nil {
		t.Fatal("decoy fields outside the finding were accepted")
	}
}

func TestInvalidResultCausePrefersParseError(t *testing.T) {
	cause := incompleteCause(AttemptInvalidResult, "deprecation warning", errors.New("review result is missing END_REVIEW"))
	if !strings.HasPrefix(cause, "review result is missing END_REVIEW") {
		t.Fatalf("cause = %q", cause)
	}
}
