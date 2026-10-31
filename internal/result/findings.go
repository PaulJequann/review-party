package result

import (
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"strings"
)

// wireFinding is the on-the-wire shape of one finding.
type wireFinding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Location string `json:"location"`
	Failure  string `json:"failure"`
	Evidence string `json:"evidence"`
	Fix      string `json:"fix"`
	Test     string `json:"test"`
}

// salvageReason explains why one finding was dropped when an
// otherwise-usable findings result was salvaged.
type salvageReason struct {
	findingIndex int
	cause        error
}

// parseFindings validates every finding. A semantically malformed finding is
// recorded as a salvage reason instead of failing the whole review, but the
// salvage slice is non-empty whenever any finding was dropped, so the caller
// cannot mistake partial output for a fully valid parse.
func parseFindings(caveats []string, wire []wireFinding) (model.ReviewResult, error) {
	if len(wire) == 0 || len(wire) > 8 {
		return model.ReviewResult{}, errors.New("findings review must contain one to eight findings")
	}
	findings := make([]model.Finding, 0, len(wire))
	salvage := []salvageReason{}
	for index, candidate := range wire {
		finding, err := validateFinding(index, candidate)
		if err != nil {
			salvage = append(salvage, salvageReason{findingIndex: index, cause: err})
			continue
		}
		findings = append(findings, finding)
	}
	if len(findings) == 0 {
		// Nothing usable survived, so there is no partial evidence to
		// preserve; the strict failure stands.
		return model.ReviewResult{}, errors.New("findings review must contain one to eight findings")
	}
	if len(salvage) == 0 {
		return model.ReviewResult{
			Status:   model.ResultFindings,
			Summary:  fmt.Sprintf("%d actionable finding(s).", len(findings)),
			Caveats:  caveats,
			Findings: findings,
		}, nil
	}
	return partialFindings(caveats, findings, salvage)
}

// partialFindings builds the explicitly-partial result and the paired
// incompleteness error. The error names every dropped finding so no caller
// can mistake the salvage for a fully valid parse.
func partialFindings(caveats []string, findings []model.Finding, salvage []salvageReason) (model.ReviewResult, error) {
	details := make([]string, 0, len(salvage))
	for _, reason := range salvage {
		details = append(details, fmt.Sprintf("finding %d: %s", reason.findingIndex+1, reason.cause.Error()))
	}
	result := model.ReviewResult{
		Status:   model.ResultFindingsPartial,
		Summary:  fmt.Sprintf("%d actionable finding(s); at least one finding was malformed and dropped.", len(findings)),
		Caveats:  caveats,
		Findings: findings,
	}
	return result, fmt.Errorf("review result is incomplete: dropped %d malformed finding(s): %s", len(salvage), strings.Join(details, "; "))
}

// validateFinding enforces the contract's semantic rules for one finding and
// assigns its dense ordinal.
func validateFinding(index int, wire wireFinding) (model.Finding, error) {
	finding := model.Finding{
		Ordinal:  index + 1,
		Severity: strings.TrimSpace(wire.Severity),
		Category: strings.TrimSpace(wire.Category),
		Location: strings.TrimSpace(wire.Location),
		Failure:  strings.TrimSpace(wire.Failure),
		Evidence: strings.TrimSpace(wire.Evidence),
		Fix:      strings.TrimSpace(wire.Fix),
		Test:     strings.TrimSpace(wire.Test),
	}
	switch finding.Severity {
	case "MEDIUM", "HIGH", "CRITICAL":
	default:
		return model.Finding{}, fmt.Errorf(`"severity" must be MEDIUM, HIGH, or CRITICAL, got %q`, wire.Severity)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "category", value: finding.Category},
		{name: "location", value: finding.Location},
		{name: "failure", value: finding.Failure},
		{name: "evidence", value: finding.Evidence},
		{name: "fix", value: finding.Fix},
		{name: "test", value: finding.Test},
	} {
		if field.value == "" {
			return model.Finding{}, fmt.Errorf("finding %d requires a non-empty %q", index+1, field.name)
		}
	}
	return finding, nil
}
