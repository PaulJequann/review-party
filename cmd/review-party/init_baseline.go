package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"reviewparty/internal/configuration"
)

// completeBaseline publishes the missing baseline Profiles, then the Global
// Party baseline.
func (setup initSetup) completeBaseline(current configuration.ReviewSelection) (configuration.Baseline, int) {
	execution := *setup.options.baseline
	baseline, err := initBaseline(setup.manager, setup.repository, execution)
	if err != nil {
		return configuration.Baseline{}, printFailure(setup.streams.errors, err)
	}
	if err := printBaselineNotes(setup.streams.output, baseline, current, execution); err != nil {
		return configuration.Baseline{}, printFailure(setup.streams.errors, err)
	}
	steps := []func() (configuration.Plan, error){
		func() (configuration.Plan, error) {
			return setup.manager.PlanBaselineProfiles(setup.repository, execution)
		},
		func() (configuration.Plan, error) { return setup.manager.PlanBaselineParty(setup.repository) },
	}
	for _, step := range steps {
		plan, err := step()
		if err != nil {
			return configuration.Baseline{}, printFailure(setup.streams.errors, err)
		}
		if plan.Unchanged() {
			continue
		}
		if code := setup.publish(plan); code != 0 {
			return configuration.Baseline{}, code
		}
	}
	return baseline, 0
}

// initBaseline reads the baseline and refuses, before anything is written,
// when it is blocked or when missing members have no execution to share.
func initBaseline(manager *configuration.Manager, repository configuration.Repository, execution configuration.ProfileExecution) (configuration.Baseline, error) {
	baseline, err := manager.Baseline(repository)
	if err != nil {
		return configuration.Baseline{}, err
	}
	if !baseline.Offered() {
		return configuration.Baseline{}, errors.New("no Templates belong to the Review Party baseline")
	}
	if blocked := baseline.Blocked(); blocked != nil {
		return configuration.Baseline{}, fmt.Errorf("Review Party baseline blocked: %w", blocked)
	}
	missing := configuration.BaselineNames(baseline.MembersIn(configuration.BaselineMissing))
	if len(missing) > 0 && !execution.Complete() {
		return configuration.Baseline{}, fmt.Errorf("Global Profiles %s are missing; add --reviewer --model --effort --deadline to create them (review-party config discover <reviewer> lists models)",
			strings.Join(missing, ", "))
	}
	return baseline, nil
}

// printBaselineNotes names what init --baseline keeps as it is.
func printBaselineNotes(writer io.Writer, baseline configuration.Baseline, current configuration.ReviewSelection, execution configuration.ProfileExecution) error {
	return writeCommandOutput(writer, func(output *commandOutput) {
		for _, note := range baseline.Notes(current) {
			output.write("%s\n", note)
		}
		if execution.Complete() && len(baseline.MembersIn(configuration.BaselineMissing)) == 0 {
			output.write("Every baseline Profile exists; --reviewer, --model, --effort, and --deadline were not used.\n")
		}
	})
}
