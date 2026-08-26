package engine

import (
	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

// ApplyEvalConfigurationDefaults applies the authored Global Configuration
// defaults for the reviewer, model, retry policy, and concurrency limit of an
// Experiment Configuration.
func ApplyEvalConfigurationDefaults(globalConfigPath string, experiment *model.ExperimentConfiguration) error {
	manager := newConfigurationManager(globalConfigPath)
	effective, err := manager.Resolve(configuration.Request{})
	if err != nil {
		return err
	}
	applyConfiguredEvalReviewer(effective, experiment)
	applyConfiguredEvalPolicy(effective.Eval, experiment)
	return nil
}

func applyConfiguredEvalReviewer(effective configuration.Effective, experiment *model.ExperimentConfiguration) {
	if experiment.Reviewer == "" && effective.DefaultReviewer.Authored {
		experiment.Reviewer = effective.DefaultReviewer.Value
	}
	if experiment.Model != "" || experiment.Reviewer == "" {
		return
	}
	settings, configured := effective.ReviewerPolicy(experiment.Reviewer)
	if configured && settings.Model.Authored {
		experiment.Model = settings.Model.Value
	}
}

func applyConfiguredEvalPolicy(value configuration.Value[configuration.EvalPolicy], experiment *model.ExperimentConfiguration) {
	if !value.Authored {
		return
	}
	policy := value.Value
	if experiment.RetryPolicy.MaxAttempts == 0 && policy.RetryPolicy.MaxAttempts != 0 {
		experiment.RetryPolicy = model.RetryPolicy{
			MaxAttempts:    policy.RetryPolicy.MaxAttempts,
			InitialBackoff: policy.RetryPolicy.InitialBackoff,
			MaxBackoff:     policy.RetryPolicy.MaxBackoff,
		}
	}
	if experiment.ConcurrencyLimit == 0 && policy.ConcurrencyLimit != 0 {
		experiment.ConcurrencyLimit = policy.ConcurrencyLimit
	}
}
