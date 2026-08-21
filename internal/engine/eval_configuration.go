package engine

import (
	"reviewparty/internal/model"
)

type userEvalPolicy struct {
	RetryPolicy      model.RetryPolicy `json:"retry_policy,omitempty"`
	ConcurrencyLimit int               `json:"concurrency_limit,omitempty"`
}

func (policy *userEvalPolicy) UnmarshalJSON(payload []byte) error {
	type plainPolicy userEvalPolicy
	var decoded plainPolicy
	if err := decodeStrictObject(payload, &decoded, "eval policy", "retry_policy"); err != nil {
		return err
	}
	*policy = userEvalPolicy(decoded)
	return nil
}

func ApplyEvalConfigurationDefaults(path string, experiment *model.ExperimentConfiguration) error {
	configuration, err := loadUserConfiguration(path)
	if err != nil {
		return err
	}
	applyConfiguredEvalReviewer(configuration, experiment)
	applyConfiguredEvalPolicy(configuration.Eval, experiment)
	return nil
}

func applyConfiguredEvalReviewer(configuration userConfiguration, experiment *model.ExperimentConfiguration) {
	if experiment.Reviewer == "" && configuration.DefaultReviewer != "" {
		experiment.Reviewer = configuration.DefaultReviewer
	}
	if experiment.Model != "" || experiment.Reviewer == "" {
		return
	}
	policy, configured := configuration.Reviewers[experiment.Reviewer]
	if configured && policy.Model.present {
		experiment.Model = policy.Model.value
	}
}

func applyConfiguredEvalPolicy(policy userEvalPolicy, experiment *model.ExperimentConfiguration) {
	if experiment.RetryPolicy.MaxAttempts == 0 && policy.RetryPolicy.MaxAttempts != 0 {
		experiment.RetryPolicy = policy.RetryPolicy
	}
	if experiment.ConcurrencyLimit == 0 && policy.ConcurrencyLimit != 0 {
		experiment.ConcurrencyLimit = policy.ConcurrencyLimit
	}
}
