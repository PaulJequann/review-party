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

// salvageReason explains why one finding section was dropped when an
// otherwise-usable findings review was salvaged.
type salvageReason struct {
	sectionIndex int
	cause        error
}

var findingHeader = regexp.MustCompile(`(?m)^(\d+)\. (MEDIUM|HIGH|CRITICAL) \| ([^|]+) \| (.+)$`)

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
		return nil, errors.New("review result has no status line")
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
	return model.ReviewResult{Status: model.ResultClean, Summary: "No actionable findings.", Findings: []model.Finding{}, Raw: review}, nil
}

// parseFindings validates a findings review strictly first. When strict
// parsing fails only on malformed finding sections and at least one
// well-formed finding survives, it returns the salvaged partial result
// together with an incompleteness error; the caller decides whether that
// partial evidence is acceptable. Everything else stays a hard error.
func parseFindings(review string, body []string) (model.ReviewResult, error) {
	if countExact(body, "status: findings") != 1 || countExact(body, "status: clean") != 0 {
		return model.ReviewResult{}, errors.New("findings review has contradictory status")
	}
	headerLocations := findingHeader.FindAllStringSubmatchIndex(review, -1)
	if err := validateFindingCount(headerLocations); err != nil {
		return model.ReviewResult{}, err
	}
	findings, salvage, err := parseFindingSections(review, headerLocations)
	if err != nil {
		return model.ReviewResult{}, err
	}
	if len(salvage) == 0 {
		return model.ReviewResult{
			Status:   model.ResultFindings,
			Summary:  fmt.Sprintf("%d actionable finding(s).", len(findings)),
			Findings: findings,
			Raw:      review,
		}, nil
	}
	if len(findings) == 0 {
		// Nothing usable survived, so there is no partial evidence to
		// preserve; the strict failure stands.
		return model.ReviewResult{}, errors.New("findings review must contain one to eight findings")
	}
	return partialFindings(review, findings, salvage)
}

// partialFindings builds the explicitly-partial result and the paired
// incompleteness error. The error names every dropped section so no caller
// can mistake the salvage for a fully clean parse.
func partialFindings(review string, findings []model.Finding, salvage []salvageReason) (model.ReviewResult, error) {
	details := make([]string, 0, len(salvage))
	for _, reason := range salvage {
		details = append(details, fmt.Sprintf("section %d: %s", reason.sectionIndex+1, reason.cause.Error()))
	}
	result := model.ReviewResult{
		Status:   model.ResultFindingsPartial,
		Summary:  fmt.Sprintf("%d actionable finding(s); at least one finding section was malformed and dropped.", len(findings)),
		Findings: findings,
		Raw:      review,
	}
	return result, fmt.Errorf("review result is incomplete: dropped %d malformed finding section(s): %s", len(salvage), strings.Join(details, "; "))
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
