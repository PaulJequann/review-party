package result

import (
	"strings"
	"testing"
)

func TestReviewAcceptsCRLFFindings(t *testing.T) {
	review := strings.ReplaceAll(findingsReviewExample, "\n", "\r\n")
	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount != 1 {
		t.Fatalf("result = %#v, want one finding", result)
	}
}

func TestInlineMarkerNamesDoNotReplaceTerminalReviewBlock(t *testing.T) {
	review := strings.Replace(findingsReviewExample, "Fix: Update the caller with the state change.", "Fix: Preserve literal `BEGIN_REVIEW` and `END_REVIEW` examples.", 1)
	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount != 1 {
		t.Fatalf("result = %#v, want one finding", result)
	}
}

func TestMultipleReviewBlocksAreInvalid(t *testing.T) {
	_, err := parseReviewResult(findingsReviewExample + "\n" + cleanReviewExample)
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
