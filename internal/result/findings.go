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

func parseFindingSections(review string, locations [][]int) ([]model.Finding, error) {
	findings := make([]model.Finding, 0, len(locations))
	for index, location := range locations {
		end := len(review)
		if index+1 < len(locations) {
			end = locations[index+1][0]
		}
		finding, err := parseFinding(findingSection(review[location[0]:end]))
		if err != nil {
			return nil, err
		}
		if finding.Ordinal != index+1 {
			return nil, errors.New("finding ordinals must begin at 1 and increase by 1")
		}
		findings = append(findings, finding)
	}
	return findings, nil
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
