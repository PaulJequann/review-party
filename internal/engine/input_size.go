package engine

import (
	"fmt"
	"unicode/utf8"

	"reviewparty/internal/model"
)

const inputWarningPercent = 60

type inputRejection struct {
	characters int
	limit      int
	line       string
}

func (rejection inputRejection) message() string {
	if rejection.characters > 0 && rejection.limit > 0 {
		return inputTooLargeMessage(rejection.characters, rejection.limit)
	}
	return compactDiagnostic(rejection.line)
}

func inputTooLargeMessage(characters, limit int) string {
	return fmt.Sprintf("reviewer input is %d characters and the limit is %d", characters, limit)
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
