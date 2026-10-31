package result

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const cleanResultExample = `{"status":"clean","caveats":[],"findings":[]}`

const findingsResultExample = `{"status":"findings","caveats":[],"findings":[
  {"severity":"HIGH","category":"correctness","location":"review.go:3","failure":"The changed state is not handled.","evidence":"The Subject changes the state without updating its caller.","fix":"Update the caller with the state change.","test":"Exercise the caller with the changed state."}
]}`

func TestCleanResultParses(t *testing.T) {
	result, err := parseReviewResult(cleanResultExample)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultClean || result.Summary != "No actionable findings." {
		t.Fatalf("result = %#v, want a clean result", result)
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

func TestCleanResultRecordsCaveats(t *testing.T) {
	result, err := parseReviewResult(`{"status":"clean","caveats":[" Could not run tests: no shell. ","Second limit"],"findings":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Could not run tests: no shell.", "Second limit"}
	if !reflect.DeepEqual(result.Caveats, want) {
		t.Fatalf("caveats = %#v, want %#v", result.Caveats, want)
	}
	if result.Summary != "No actionable findings." {
		t.Fatalf("summary = %q, want the fixed clean summary", result.Summary)
	}
}

func TestFencedResultParses(t *testing.T) {
	fenced := "```json\n" + findingsResultExample + "\n```"
	result, err := parseReviewResult(fenced)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFindings || result.FindingCount() != 1 {
		t.Fatalf("result = %#v, want one finding", result)
	}
}

func TestNarrationAroundResultIsTolerated(t *testing.T) {
	for _, valid := range []string{
		"Here is my review:\n" + cleanResultExample,
		cleanResultExample + "\nDone.",
		"```json\n" + findingsResultExample + "\n```",
	} {
		if _, err := parseReviewResult(valid); err != nil {
			t.Fatalf("valid result was rejected: %q: %v", valid, err)
		}
	}
}

func TestNonObjectResultsAreRejected(t *testing.T) {
	for _, invalid := range []string{
		"",
		"Here is my review, no object.",
		"[]",
	} {
		if _, err := parseReviewResult(invalid); err == nil {
			t.Fatalf("invalid result was accepted: %q", invalid)
		}
	}
}

func TestMultipleResultObjectsAreRejected(t *testing.T) {
	if _, err := parseReviewResult(cleanResultExample + "\n" + cleanResultExample); err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("error = %v, want a multiple-object rejection", err)
	}
	separated := cleanResultExample + "\nActually, also consider:\n```\n" + findingsResultExample + "\n```"
	if _, err := parseReviewResult(separated); err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("error = %v, want a second object after narration rejected", err)
	}
	if _, err := parseReviewResult(cleanResultExample + "\nDone {thanks}"); err != nil {
		t.Fatalf("brace in trailing prose was misread as a second object: %v", err)
	}
}

func TestMissingOrNullFindingsAreRejected(t *testing.T) {
	for _, invalid := range []string{
		`{"status":"clean"}`,
		`{"status":"clean","findings":null}`,
		`{"findings":[]}`,
	} {
		if _, err := parseReviewResult(invalid); err == nil {
			t.Fatalf("invalid result was accepted: %q", invalid)
		}
	}
}

func TestDuplicateFieldsAreRejected(t *testing.T) {
	duplicated := `{"status":"findings","findings":[` +
		`{"severity":"HIGH","category":"c","location":"l","failure":"f","evidence":"e","fix":"x","test":"t"}` +
		`],"status":"clean","findings":[]}`
	_, err := parseReviewResult(duplicated)
	if err == nil || !strings.Contains(err.Error(), `field "status" is declared more than once`) {
		t.Fatalf("error = %v, want a duplicate-field rejection", err)
	}
	nested := strings.Replace(findingsResultExample, `"failure":"`, `"failure":"dup","failure":"`, 1)
	if _, err := parseReviewResult(nested); err == nil {
		t.Fatal("duplicate nested field was accepted")
	}
}

func TestSchemaRequiresEveryDeclaredProperty(t *testing.T) {
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Required []string `json:"required"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(CanonicalReviewResultContract.Schema()), &schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range schema.Required {
		if _, ok := schema.Properties[name]; !ok {
			t.Fatalf("required property %q is not declared", name)
		}
	}
	for _, want := range []string{"status", "caveats", "findings"} {
		if !slices.Contains(schema.Required, want) {
			t.Fatalf("schema required = %v, want %q included for strict structured outputs", schema.Required, want)
		}
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	_, err := parseReviewResult(`{"status":"clean","summry":"No actionable findings.","findings":[]}`)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want an unknown-field rejection", err)
	}
}

func TestInvalidStatusIsRejected(t *testing.T) {
	_, err := parseReviewResult(`{"status":"maybe","findings":[]}`)
	if err == nil || !strings.Contains(err.Error(), `"status" must be`) {
		t.Fatalf("error = %v, want a status rejection", err)
	}
}

func TestCleanResultWithFindingsIsRejected(t *testing.T) {
	_, err := parseReviewResult(`{"status":"clean","caveats":[],"findings":[{"severity":"HIGH","category":"c","location":"l","failure":"f","evidence":"e","fix":"x","test":"t"}]}`)
	if err == nil || !strings.Contains(err.Error(), "clean review must have an empty") {
		t.Fatalf("error = %v, want a contradictory-status rejection", err)
	}
}

func TestFindingsResultParses(t *testing.T) {
	result, err := parseReviewResult(findingsResultExample)
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{Ordinal: 1, Severity: "HIGH", Category: "correctness", Location: "review.go:3", Failure: "The changed state is not handled.", Evidence: "The Subject changes the state without updating its caller.", Fix: "Update the caller with the state change.", Test: "Exercise the caller with the changed state."},
	}
	if !reflect.DeepEqual(result.Findings, want) {
		t.Fatalf("findings = %#v, want %#v", result.Findings, want)
	}
	if result.Summary != "1 actionable finding(s)." {
		t.Fatalf("summary = %q, want a synthesized count", result.Summary)
	}
}

func repeatFinding(ordinal int, location string) string {
	return fmt.Sprintf(`{"severity":"MEDIUM","category":"c","location":"%s","failure":"Failure %d.","evidence":"Evidence %d.","fix":"Fix %d.","test":"Test %d."}`, location, ordinal, ordinal, ordinal, ordinal)
}

func TestFindingsAreNumberedByReviewParty(t *testing.T) {
	result, err := parseReviewResult(`{"status":"findings","caveats":[],"findings":[` +
		repeatFinding(1, "first.go:1") + "," + repeatFinding(2, "second.go:2") + `]}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Findings[0].Ordinal != 1 || result.Findings[1].Ordinal != 2 {
		t.Fatalf("ordinals = %d,%d, want 1,2", result.Findings[0].Ordinal, result.Findings[1].Ordinal)
	}
}

func TestTooManyCaveatsAreRejected(t *testing.T) {
	_, err := parseReviewResult(`{"status":"clean","caveats":["a","b","c","d","e"],"findings":[]}`)
	if err == nil || !strings.Contains(err.Error(), `"caveats" holds at most`) {
		t.Fatalf("error = %v, want a caveat-count rejection", err)
	}
}

func TestStructuredFindingsRoundTripThroughJSON(t *testing.T) {
	result, err := parseReviewResult(findingsResultExample)
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
}
