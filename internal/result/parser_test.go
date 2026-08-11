package result

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReviewAcceptsCRLFFindings(t *testing.T) {
	review := strings.ReplaceAll(findingsReviewExample, "\n", "\r\n")
	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount() != 1 {
		t.Fatalf("result = %#v, want one finding", result)
	}
}

func TestInlineMarkerNamesDoNotReplaceTerminalReviewBlock(t *testing.T) {
	review := strings.Replace(findingsReviewExample, "Fix: Update the caller with the state change.", "Fix: Preserve literal `BEGIN_REVIEW` and `END_REVIEW` examples.", 1)
	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount() != 1 {
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

func TestFindingsPreserveFieldsInSourceOrder(t *testing.T) {
	review := `BEGIN_REVIEW
status: findings

1. HIGH | correctness | first.go:3
Failure: First failure.
Evidence: First evidence.
Fix: First correction.
Test: First regression.

2. MEDIUM | maintainability | second.go:8
Failure: Second failure.
Evidence: Second evidence.
Fix: Second correction.
Test: Second regression.
END_REVIEW`

	result, err := parseReviewResult(review)
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{Ordinal: 1, Severity: "HIGH", Category: "correctness", Location: "first.go:3", Failure: "First failure.", Evidence: "First evidence.", Fix: "First correction.", Test: "First regression."},
		{Ordinal: 2, Severity: "MEDIUM", Category: "maintainability", Location: "second.go:8", Failure: "Second failure.", Evidence: "Second evidence.", Fix: "Second correction.", Test: "Second regression."},
	}
	if !reflect.DeepEqual(result.Findings, want) {
		t.Fatalf("findings = %#v, want %#v", result.Findings, want)
	}
}

func TestFindingRequiresExactlyOneOfEachEvidenceField(t *testing.T) {
	for _, invalid := range []struct {
		field       string
		replacement string
	}{
		{field: "Evidence: Why the Subject permits the failure.", replacement: ""},
		{field: "Test: Regression that fails before the correction.", replacement: "Test: First regression.\nTest: Duplicate regression."},
	} {
		t.Run(invalid.field, func(t *testing.T) {
			review := strings.Replace(findingsReviewExample, invalid.field, invalid.replacement, 1)
			if _, err := parseReviewResult(review); err == nil {
				t.Fatal("invalid finding fields were accepted")
			}
		})
	}
}

func TestCleanReviewHasEmptyFindings(t *testing.T) {
	result, err := parseReviewResult(cleanReviewExample)
	if err != nil {
		t.Fatal(err)
	}
	if result.Findings == nil || len(result.Findings) != 0 || result.FindingCount() != 0 {
		t.Fatalf("result = %#v, want an empty findings collection", result)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"findings":[]`) {
		t.Fatalf("JSON = %s, want an empty findings collection", payload)
	}
}

func TestStructuredFindingsRoundTripThroughJSON(t *testing.T) {
	result, err := parseReviewResult(findingsReviewExample)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var loaded ReviewResult
	if err := json.Unmarshal(payload, &loaded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, result) {
		t.Fatalf("loaded = %#v, want %#v", loaded, result)
	}
	if loaded.FindingCount() != result.FindingCount() {
		t.Fatalf("loaded count = %d, want %d", loaded.FindingCount(), result.FindingCount())
	}
}
