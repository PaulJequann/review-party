package result

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

// partialReviewExample carries one well-formed finding followed by a finding
// section whose Evidence field is missing, so the first finding survives
// strict parsing and the second is salvageable evidence.
const partialReviewExample = `BEGIN_REVIEW
status: findings

1. HIGH | correctness | first.go:3
Failure: First failure.
Evidence: First evidence.
Fix: First correction.
Test: First regression.

2. MEDIUM | maintainability | second.go:8
Failure: Second failure.
Fix: Second correction.
Test: Second regression.
END_REVIEW`

func TestSalvagesPartialFindingsReviewWithPairedError(t *testing.T) {
	result, err := parseReviewResult(partialReviewExample)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	if !strings.Contains(err.Error(), "incomplete") || !strings.Contains(err.Error(), "section 2") {
		t.Fatalf("error = %v, want the dropped-section incompleteness report", err)
	}
	if result.Status != ResultFindingsPartial {
		t.Fatalf("status = %q, want %q", result.Status, ResultFindingsPartial)
	}
	want := []Finding{
		{Ordinal: 1, Severity: "HIGH", Category: "correctness", Location: "first.go:3", Failure: "First failure.", Evidence: "First evidence.", Fix: "First correction.", Test: "First regression."},
	}
	if !reflect.DeepEqual(result.Findings, want) {
		t.Fatalf("findings = %#v, want only the salvaged first finding", result.Findings)
	}
	if !strings.Contains(result.Summary, "malformed and dropped") {
		t.Fatalf("summary = %q, want an explicit partial warning", result.Summary)
	}
}

func TestSalvageRenumbersSurvivingFindings(t *testing.T) {
	// The first section is the malformed one, so the surviving finding is
	// renumbered from its original 2 to 1.
	review := `BEGIN_REVIEW
status: findings

1. HIGH | correctness | broken.go:1
Failure: Broken failure.

2. HIGH | correctness | healthy.go:3
Failure: Healthy failure.
Evidence: Healthy evidence.
Fix: Healthy correction.
Test: Healthy regression.
END_REVIEW`
	result, err := parseReviewResult(review)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	if len(result.Findings) != 1 || result.Findings[0].Ordinal != 1 || result.Findings[0].Location != "healthy.go:3" {
		t.Fatalf("findings = %#v, want the surviving finding renumbered to 1", result.Findings)
	}
}

func TestSalvageKeepsWholeReviewInvariantsStrict(t *testing.T) {
	// Section 3 skips an ordinal, so the whole-review invariant is broken and
	// salvage must not paper over it, even though both sections parse.
	review := `BEGIN_REVIEW
status: findings

3. HIGH | correctness | first.go:3
Failure: First failure.
Evidence: First evidence.
Fix: First correction.
Test: First regression.

4. HIGH | correctness | second.go:8
Failure: Second failure.
Evidence: Second evidence.
Fix: Second correction.
Test: Second regression.
END_REVIEW`
	if _, err := parseReviewResult(review); err == nil || strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v, want a hard ordinal error without salvage", err)
	}
}

func TestAllMalformedSectionsLeaveNothingToSalvage(t *testing.T) {
	review := strings.Replace(findingsReviewExample, "Evidence: Why the Subject permits the failure.", "", 1)
	result, err := parseReviewResult(review)
	if err == nil {
		t.Fatal("fully malformed review was salvaged")
	}
	if result.Status == ResultFindingsPartial || len(result.Findings) != 0 {
		t.Fatalf("result = %#v, want no salvaged result", result)
	}
	if !strings.Contains(err.Error(), "one to eight findings") {
		t.Fatalf("error = %v, want the strict count error", err)
	}
}

func TestContradictoryStatusIsNeverSalvaged(t *testing.T) {
	review := `BEGIN_REVIEW
status: findings
status: clean
END_REVIEW`
	result, err := parseReviewResult(review)
	if err == nil {
		t.Fatal("contradictory review was salvaged")
	}
	if result.Status == ResultFindingsPartial || len(result.Findings) != 0 {
		t.Fatalf("result = %#v, want no salvaged result", result)
	}
}

func TestPartialResultRoundTripsThroughJSON(t *testing.T) {
	result, err := parseReviewResult(partialReviewExample)
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
	if !reflect.DeepEqual(loaded, result) {
		t.Fatalf("loaded = %#v, want %#v", loaded, result)
	}
	if loaded.Status != model.ResultFindingsPartial {
		t.Fatalf("status = %q, want %q", loaded.Status, model.ResultFindingsPartial)
	}
}

func TestSalvageSurvivesMalformedHeaderBetweenValidFindings(t *testing.T) {
	// The second section has an unsupported severity in its header. The
	// lenient anchor still delimits it, so the strict header failure only
	// drops section 2 and both neighbouring findings survive.
	review := `BEGIN_REVIEW
status: findings

1. HIGH | correctness | first.go:3
Failure: First failure.
Evidence: First evidence.
Fix: First correction.
Test: First regression.

2. LOW | maintainability | second.go:8
Failure: Second failure.
Evidence: Second evidence.
Fix: Second correction.
Test: Second regression.

3. HIGH | correctness | third.go:12
Failure: Third failure.
Evidence: Third evidence.
Fix: Third correction.
Test: Third regression.
END_REVIEW`
	result, err := parseReviewResult(review)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	if !strings.Contains(err.Error(), "section 2") {
		t.Fatalf("error = %v, want section 2 to be the dropped one", err)
	}
	if result.Status != ResultFindingsPartial {
		t.Fatalf("status = %q, want %q", result.Status, ResultFindingsPartial)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("findings = %#v, want the two well-formed neighbours", result.Findings)
	}
	if result.Findings[0].Location != "first.go:3" || result.Findings[1].Location != "third.go:12" {
		t.Fatalf("findings = %#v, want sections 1 and 3 salvaged", result.Findings)
	}
	if result.Findings[0].Ordinal != 1 || result.Findings[1].Ordinal != 2 {
		t.Fatalf("ordinals = %d, %d; want renumbered 1, 2", result.Findings[0].Ordinal, result.Findings[1].Ordinal)
	}
}

func TestSalvageSurvivesMalformedFirstHeader(t *testing.T) {
	// The first section's header is missing its severity entirely, so the
	// old boundary detection failed the ordinal check outright; now only
	// section 1 is dropped and the well-formed second finding survives.
	review := `BEGIN_REVIEW
status: findings

1. correctness | broken.go:1
Failure: Broken failure.
Evidence: Broken evidence.
Fix: Broken correction.
Test: Broken regression.

2. HIGH | correctness | healthy.go:3
Failure: Healthy failure.
Evidence: Healthy evidence.
Fix: Healthy correction.
Test: Healthy regression.
END_REVIEW`
	result, err := parseReviewResult(review)
	if err == nil {
		t.Fatal("partial review was accepted without an incompleteness error")
	}
	if !strings.Contains(err.Error(), "section 1") {
		t.Fatalf("error = %v, want section 1 to be the dropped one", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Location != "healthy.go:3" {
		t.Fatalf("findings = %#v, want the surviving second finding", result.Findings)
	}
	if result.Findings[0].Ordinal != 1 {
		t.Fatalf("ordinal = %d, want renumbered to 1", result.Findings[0].Ordinal)
	}
}
