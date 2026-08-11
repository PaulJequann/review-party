package result

import (
	"errors"
	"fmt"
	"regexp"
	"reviewparty/internal/model"
	"strings"
)

const (
	beginReview   = "BEGIN_REVIEW"
	endReview     = "END_REVIEW"
	MaxResultSize = 24_576
)

var findingHeader = regexp.MustCompile(`(?m)^\d+\. (MEDIUM|HIGH|CRITICAL) \| [^|]+ \| .+$`)

func parseReviewResult(assistantText string) (model.ReviewResult, error) {
	review, err := extractLastReview(assistantText)
	if err != nil {
		return model.ReviewResult{}, err
	}
	if len(review) > MaxResultSize {
		return model.ReviewResult{}, fmt.Errorf("review result exceeds %d bytes", MaxResultSize)
	}

	review = strings.ReplaceAll(strings.TrimSpace(review), "\r\n", "\n")
	body, err := reviewBody(review)
	if err != nil {
		return model.ReviewResult{}, err
	}

	switch body[0] {
	case "status: clean":
		return parseCleanReview(review, body)
	case "status: findings":
		return parseFindings(review, body)
	default:
		return model.ReviewResult{}, errors.New("review must begin with status: clean or status: findings")
	}
}

func reviewBody(review string) ([]string, error) {
	lines := strings.Split(review, "\n")
	if len(lines) < 3 {
		return nil, errors.New("review markers are malformed")
	}
	if lines[0] != beginReview {
		return nil, errors.New("review markers are malformed")
	}
	if lines[len(lines)-1] != endReview {
		return nil, errors.New("review markers are malformed")
	}
	body := nonemptyLines(lines[1 : len(lines)-1])
	if len(body) == 0 {
		return nil, errors.New("review result has no status")
	}
	return body, nil
}

func parseCleanReview(review string, body []string) (model.ReviewResult, error) {
	if len(body) != 2 {
		return model.ReviewResult{}, errors.New("clean review contains unexpected content")
	}
	if body[1] != "summary: No actionable findings." {
		return model.ReviewResult{}, errors.New("clean review contains unexpected content")
	}
	return model.ReviewResult{Status: model.ResultClean, Summary: "No actionable findings.", Raw: review}, nil
}

func parseFindings(review string, body []string) (model.ReviewResult, error) {
	if countExact(body, "status: findings") != 1 || countExact(body, "status: clean") != 0 {
		return model.ReviewResult{}, errors.New("findings review has contradictory status")
	}
	headerLocations := findingHeader.FindAllStringIndex(review, -1)
	if err := validateFindingCount(headerLocations); err != nil {
		return model.ReviewResult{}, err
	}
	if err := validateFindingSections(review, headerLocations); err != nil {
		return model.ReviewResult{}, err
	}
	return model.ReviewResult{
		Status:       model.ResultFindings,
		Summary:      fmt.Sprintf("%d actionable finding(s).", len(headerLocations)),
		FindingCount: len(headerLocations),
		Raw:          review,
	}, nil
}

func validateFindingCount(locations [][]int) error {
	if len(locations) == 0 {
		return errors.New("findings review must contain one to eight findings")
	}
	if len(locations) > 8 {
		return errors.New("findings review must contain one to eight findings")
	}
	return nil
}

func validateFindingSections(review string, locations [][]int) error {
	for index, location := range locations {
		end := len(review)
		if index+1 < len(locations) {
			end = locations[index+1][0]
		}
		section := review[location[0]:end]
		for _, field := range []string{"Failure:", "Evidence:", "Fix:", "Test:"} {
			if strings.Count(section, "\n"+field) != 1 {
				return fmt.Errorf("each finding must contain one %s field", field)
			}
		}
	}
	return nil
}

func extractLastReview(text string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if markerLineCount(lines, beginReview) > 1 || markerLineCount(lines, endReview) > 1 {
		return "", errors.New("review result contains multiple review blocks")
	}
	end := lastMarkerLine(lines, endReview, len(lines)-1)
	if end == -1 {
		return "", errors.New("review result is missing END_REVIEW")
	}
	begin := lastMarkerLine(lines, beginReview, end-1)
	if begin == -1 {
		return "", errors.New("review result is missing BEGIN_REVIEW")
	}
	return strings.TrimSpace(strings.Join(lines[begin:end+1], "\n")), nil
}

func markerLineCount(lines []string, marker string) int {
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == marker {
			count++
		}
	}
	return count
}

func lastMarkerLine(lines []string, marker string, start int) int {
	for index := start; index >= 0; index-- {
		if strings.TrimSpace(lines[index]) == marker {
			return index
		}
	}
	return -1
}

func nonemptyLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return result
}

func countExact(lines []string, target string) int {
	count := 0
	for _, line := range lines {
		if line == target {
			count++
		}
	}
	return count
}
