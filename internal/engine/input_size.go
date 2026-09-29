package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"reviewparty/internal/model"
)

const codexInputCharacterLimit = 1048576

const inputWarningPercent = 60

var (
	rejectedCharactersPattern = regexp.MustCompile(`"actual_chars":\s*(\d+)`)
	rejectedLimitPattern      = regexp.MustCompile(`"max_chars":\s*(\d+)`)
)

func isInputTooLargeDiagnostic(normalized string) bool {
	return strings.Contains(normalized, "input_too_large") || strings.Contains(normalized, "input exceeds the maximum length")
}

func inputTooLargeMessage(characters, limit int) string {
	return fmt.Sprintf("reviewer input is %d characters and the limit is %d", characters, limit)
}

func categoryMessage(category model.TerminationCategory, line string) string {
	if category == model.TerminationInputTooLarge {
		characters, hasCharacters := capturedNumber(rejectedCharactersPattern, line)
		limit, hasLimit := capturedNumber(rejectedLimitPattern, line)
		if hasCharacters && hasLimit {
			return inputTooLargeMessage(characters, limit)
		}
	}
	return compactDiagnostic(line)
}

func capturedNumber(pattern *regexp.Regexp, line string) (int, bool) {
	match := pattern.FindStringSubmatch(line)
	if match == nil {
		return 0, false
	}
	number, err := strconv.Atoi(match[1])
	return number, err == nil
}

func (runner *reviewRunner) preflightInput(reviewer reviewerRegistration, prompt string) *model.ReviewTermination {
	limit := reviewer.inputCharacterLimit
	if limit <= 0 {
		return nil
	}
	characters := utf8.RuneCountInString(prompt)
	if characters > limit {
		return &model.ReviewTermination{Category: model.TerminationInputTooLarge, Phase: model.PhaseInputPreflight, Message: inputTooLargeMessage(characters, limit)}
	}
	percent := characters * 100 / limit
	if percent >= inputWarningPercent && runner.warn != nil {
		runner.warn(fmt.Sprintf("warning: %s input is %d characters, %d%% of its %d character limit", reviewer.candidate.ID, characters, percent, limit))
	}
	return nil
}
