package result

import (
	"encoding/json"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"strings"
)

const (
	MaxResultSize = 24_576
	maxCaveats    = 4
)

// wireResult is the on-the-wire shape of the structured result contract.
// Ordinals are absent on purpose: Review Party numbers findings, so an agent
// cannot drift the sequence.
type wireResult struct {
	Status   string        `json:"status"`
	Caveats  []string      `json:"caveats"`
	Findings []wireFinding `json:"findings"`
}

// parseReviewResult validates a reviewer's final message against the
// structured result contract. Shape errors reject the whole result; a
// semantically malformed finding is dropped while its well-formed neighbours
// survive as explicitly-partial evidence, paired with an incompleteness
// error the caller must surface.
func parseReviewResult(assistantText string) (model.ReviewResult, error) {
	if len(assistantText) > MaxResultSize {
		return model.ReviewResult{}, fmt.Errorf("review result exceeds %d bytes", MaxResultSize)
	}
	wire, err := decodeWireResult(assistantText)
	if err != nil {
		return model.ReviewResult{}, err
	}
	caveats, err := validateCaveats(wire.Caveats)
	if err != nil {
		return model.ReviewResult{}, err
	}

	switch wire.Status {
	case "clean":
		if len(wire.Findings) != 0 {
			return model.ReviewResult{}, errors.New(`clean review must have an empty "findings" array; report problems as findings instead`)
		}
		return model.ReviewResult{
			Status:   model.ResultClean,
			Summary:  "No actionable findings.",
			Caveats:  caveats,
			Findings: []model.Finding{},
		}, nil
	case "findings":
		return parseFindings(caveats, wire.Findings)
	default:
		return model.ReviewResult{}, fmt.Errorf(`"status" must be "clean" or "findings", got %q`, wire.Status)
	}
}

// decodeWireResult locates and decodes the single JSON object in the
// reviewer's final message. Narration around the object — separate assistant
// messages joined by the harness decoders — is tolerated, but a second JSON
// value is rejected, matching the block contract's multiple-review rule.
func decodeWireResult(assistantText string) (wireResult, error) {
	start := strings.Index(assistantText, "{")
	if start == -1 {
		return wireResult{}, errors.New(`review result must contain one JSON object with "status", "caveats", and "findings"`)
	}
	decoder := json.NewDecoder(strings.NewReader(assistantText[start:]))
	decoder.DisallowUnknownFields()
	var wire wireResult
	if err := decoder.Decode(&wire); err != nil {
		return wireResult{}, fmt.Errorf("review result is not valid contract JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return wireResult{}, errors.New("review result must be exactly one JSON object")
	}
	return wire, nil
}

func validateCaveats(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	caveats := make([]string, 0, len(raw))
	for _, caveat := range raw {
		trimmed := strings.TrimSpace(caveat)
		if trimmed == "" {
			return nil, errors.New(`every entry in "caveats" must be a non-empty string`)
		}
		caveats = append(caveats, trimmed)
	}
	if len(caveats) > maxCaveats {
		return nil, fmt.Errorf(`"caveats" holds at most %d entries, got %d`, maxCaveats, len(caveats))
	}
	return caveats, nil
}
