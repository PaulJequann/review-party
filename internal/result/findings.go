package result

import (
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"strings"
)

type findingSection string
type findingLine string
type findingFieldLabel string

type findingField struct {
	label findingFieldLabel
	value *string
}

// parseFindingSections parses every finding section. A malformed section is
// recorded as a salvage reason instead of failing the whole review, but the
// slice of salvage reasons is non-empty whenever any section was dropped, so
// the caller cannot mistake partial output for a fully valid parse. Sections
// are delimited by lenient anchors (a numbered line) rather than by strict
// headers, so a malformed header only drops its own section.
func parseFindingSections(review string, locations []sectionLocation) ([]model.Finding, []salvageReason, error) {
	findings := make([]model.Finding, 0, len(locations))
	salvage := []salvageReason{}
	parsed := make([]model.Finding, 0, len(locations))
	ordinals := make([]int, 0, len(locations))
	for index, location := range locations {
		section := review[location.start:location.end]
		ordinal, headerErr := sectionOrdinal(findingSection(section))
		if headerErr != nil {
			// The lenient anchor guarantees an ordinal prefix, but a
			// non-numeric or missing one is a structural failure the whole
			// review cannot recover from.
			return nil, nil, headerErr
		}
		ordinals = append(ordinals, ordinal)
		finding, err := parseFinding(findingSection(section))
		if err != nil {
			salvage = append(salvage, salvageReason{sectionIndex: index, cause: err})
			continue
		}
		parsed = append(parsed, finding)
	}

	// Whole-review invariants stay strict: header ordinals must begin at 1
	// and increase by exactly 1 across every section, including the dropped
	// ones. Renumbering only the salvaged survivors (below) keeps their
	// sequence dense without inventing evidence for the dropped sections.
	if err := validateSectionOrdinals(ordinals); err != nil {
		return nil, nil, err
	}
	for index, finding := range parsed {
		finding.Ordinal = index + 1
		findings = append(findings, finding)
	}
	return findings, salvage, nil
}

// sectionOrdinal reads the header ordinal of one finding section, including
// sections that later fail strict field parsing, so the whole-review ordinal
// sequence can be validated across every declared section. The ordinal comes
// from the same lenient anchor that delimited the section; whether the rest
// of the header is well-formed is decided per-section by the salvage tier.
func sectionOrdinal(section findingSection) (int, error) {
	match := findingAnchor.FindStringSubmatch(strings.Split(string(section), "\n")[0])
	if len(match) != 2 {
		return 0, errors.New("finding header is malformed")
	}
	ordinal := 0
	if _, err := fmt.Sscanf(match[1], "%d", &ordinal); err != nil {
		return 0, fmt.Errorf("parse finding ordinal: %w", err)
	}
	return ordinal, nil
}

// validateSectionOrdinals requires the header ordinals to begin at 1 and
// increase by exactly 1 across all sections, including the dropped ones.
func validateSectionOrdinals(ordinals []int) error {
	for index, ordinal := range ordinals {
		if ordinal != index+1 {
			return errors.New("finding ordinals must begin at 1 and increase by 1")
		}
	}
	return nil
}

func parseFinding(section findingSection) (model.Finding, error) {
	lines := strings.Split(string(section), "\n")
	finding, err := parseFindingHeader(findingLine(lines[0]))
	if err != nil {
		return model.Finding{}, err
	}
	for _, line := range lines[1:] {
		if line == "" || line == endReview {
			continue
		}
		if err := setFindingField(&finding, findingLine(line)); err != nil {
			return model.Finding{}, err
		}
	}
	if err := requireFindingFields(finding); err != nil {
		return model.Finding{}, err
	}
	return finding, nil
}

func parseFindingHeader(header findingLine) (model.Finding, error) {
	match := findingHeader.FindStringSubmatch(string(header))
	if len(match) != 5 {
		return model.Finding{}, errors.New("finding header is malformed")
	}
	ordinal := 0
	if _, err := fmt.Sscanf(match[1], "%d", &ordinal); err != nil {
		return model.Finding{}, fmt.Errorf("parse finding ordinal: %w", err)
	}
	finding := model.Finding{Ordinal: ordinal, Severity: match[2], Category: strings.TrimSpace(match[3]), Location: strings.TrimSpace(match[4])}
	if finding.Category == "" || finding.Location == "" {
		return model.Finding{}, errors.New("finding header requires a category and location")
	}
	return finding, nil
}

func setFindingField(finding *model.Finding, line findingLine) error {
	for _, field := range []findingField{
		{label: "Failure:", value: &finding.Failure},
		{label: "Evidence:", value: &finding.Evidence},
		{label: "Fix:", value: &finding.Fix},
		{label: "Test:", value: &finding.Test},
	} {
		if strings.HasPrefix(string(line), string(field.label)) {
			return setUniqueFindingField(field.label, field.value, line)
		}
	}
	return fmt.Errorf("finding %d contains unexpected content", finding.Ordinal)
}

func setUniqueFindingField(label findingFieldLabel, value *string, line findingLine) error {
	if *value != "" {
		return fmt.Errorf("each finding must contain one %s field", label)
	}
	*value = strings.TrimSpace(strings.TrimPrefix(string(line), string(label)))
	if *value == "" {
		return fmt.Errorf("each finding must contain one non-empty %s field", label)
	}
	return nil
}

func requireFindingFields(finding model.Finding) error {
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "Failure:", value: finding.Failure},
		{label: "Evidence:", value: finding.Evidence},
		{label: "Fix:", value: finding.Fix},
		{label: "Test:", value: finding.Test},
	} {
		if field.value == "" {
			return fmt.Errorf("each finding must contain one %s field", field.label)
		}
	}
	return nil
}
