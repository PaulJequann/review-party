package result

import (
	"encoding/json"
	"strings"
	"testing"
)

// partialResultExample carries one well-formed finding followed by one whose
// evidence is empty, so the review parses as explicitly-partial evidence.
const partialResultExample = `{"status":"findings","caveats":[],"findings":[
  {"severity":"HIGH","category":"correctness","location":"first.go:3","failure":"First failure.","evidence":"First evidence.","fix":"First fix.","test":"First test."},
  {"severity":"HIGH","category":"correctness","location":"second.go:4","failure":"Second failure.","evidence":"","fix":"Second fix.","test":"Second test."}
]}`

func findingJSON(severity, location, evidence string) string {
	return `{"severity":"` + severity + `","category":"c","location":"` + location + `","failure":"f","evidence":"` + evidence + `","fix":"x","test":"t"}`
}

func TestPartialResultKeepsWellFormedFindingsAndStaysIncomplete(t *testing.T) {
	result, err := parseReviewResult(partialResultExample)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	if !strings.Contains(err.Error(), "dropped 1 malformed finding(s)") || !strings.Contains(err.Error(), `non-empty "evidence"`) {
		t.Fatalf("error = %v, want the dropped finding named", err)
	}
	if result.Status != ResultFindingsPartial {
		t.Fatalf("status = %q, want %q", result.Status, ResultFindingsPartial)
	}
	if result.FindingCount() != 1 || result.Findings[0].Ordinal != 1 {
		t.Fatalf("findings = %#v, want the one well-formed finding renumbered", result.Findings)
	}
	if !strings.Contains(result.Summary, "at least one finding was malformed and dropped") {
		t.Fatalf("summary = %q, want an explicit partial warning", result.Summary)
	}
}

func TestInvalidSeverityIsSalvagedPerFinding(t *testing.T) {
	result, err := parseReviewResult(`{"status":"findings","caveats":[],"findings":[` +
		findingJSON("URGENT", "first.go:1", "e") + `,` + findingJSON("HIGH", "second.go:2", "e") + `]}`)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	if result.FindingCount() != 1 || result.Findings[0].Location != "second.go:2" {
		t.Fatalf("findings = %#v, want only the valid finding kept", result.Findings)
	}
	if !strings.Contains(err.Error(), `"severity" must be MEDIUM, HIGH, or CRITICAL`) {
		t.Fatalf("error = %v, want the invalid severity named", err)
	}
}

func TestFindingsResultWithoutFindingsIsRejected(t *testing.T) {
	if _, err := parseReviewResult(`{"status":"findings","caveats":[],"findings":[]}`); err == nil {
		t.Fatal("findings review without findings was accepted")
	}
}

func TestFindingsResultWithTooManyFindingsIsRejected(t *testing.T) {
	findings := make([]string, 9)
	for index := range findings {
		findings[index] = findingJSON("HIGH", "file.go:1", "e")
	}
	if _, err := parseReviewResult(`{"status":"findings","caveats":[],"findings":[` + strings.Join(findings, ",") + `]}`); err == nil {
		t.Fatal("oversized findings review was accepted")
	}
}

func TestPartialResultSurvivesJSONRoundTrip(t *testing.T) {
	result, err := parseReviewResult(partialResultExample)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var loaded ReviewResult
	if err := json.Unmarshal(payload, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Status != ResultFindingsPartial || loaded.FindingCount() != 1 {
		t.Fatalf("loaded = %#v, want the partial result preserved", loaded)
	}
}
