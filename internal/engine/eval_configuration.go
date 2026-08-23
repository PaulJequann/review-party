package engine

import (
	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

// ApplyEvalConfigurationDefaults applies the authored Personal Configuration
// defaults for the reviewer, model, retry policy, and concurrency limit of an
// Experiment Configuration.
func ApplyEvalConfigurationDefaults(personalConfigPath string, experiment *model.ExperimentConfiguration) error {
	manager := newConfigurationManager(personalConfigPath)
	loaded, err := manager.Load(configuration.Repository(""))
	if err != nil {
		return err
	}
	applyConfiguredEvalReviewer(loaded.Personal.Document, experiment)
	applyConfiguredEvalPolicy(loaded.Personal.Document.Eval, experiment)
	return nil
}

func applyConfiguredEvalReviewer(document configuration.Document, experiment *model.ExperimentConfiguration) {
	if experiment.Reviewer == "" && document.Defaults.Reviewer != "" {
		experiment.Reviewer = document.Defaults.Reviewer
	}
	if experiment.Model != "" || experiment.Reviewer == "" {
		return
	}
	policy, configured := document.Reviewers[experiment.Reviewer]
	if configured && policy.Model != "" {
		experiment.Model = policy.Model
	}
}

func applyConfiguredEvalPolicy(policy *configuration.EvalPolicy, experiment *model.ExperimentConfiguration) {
	if policy == nil {
		return
	}
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
