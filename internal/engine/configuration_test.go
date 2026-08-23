package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

const configuredReviewers = `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"},
  "reviewers": {
    "grok": {"enabled": true, "model": "grok-4.5"},
    "opencode": {
      "enabled": true,
      "model": "meta/muse-spark-1.2-contributor",
      "allowed_models": ["meta/muse-spark-1.2-contributor", "opencode-go/deepseek-v4-flash"]
    },
    "copilot": {"enabled": false, "model": "auto"}
  }
}`

func TestUserConfigurationControlsDefaultReviewerAndModel(t *testing.T) {
	conductor := configuredTestConductor(t, configuredReviewers)

	explanation, err := conductor.Explain(context.Background(), ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if !explanation.ReviewerWasDefault {
		t.Fatal("configured default was reported as explicit")
	}
	got := explanation.ProfileRevision.Reviewer
	want := ReviewerProvenance{ReviewerID: "opencode", Model: "meta/muse-spark-1.2-contributor", Effort: "default", Harness: "opencode-cli", Transport: "direct-cli"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewer = %#v, want %#v", got, want)
	}
}

func TestDisabledReviewerFailsBeforeSubjectResolution(t *testing.T) {
	conductor := configuredTestConductor(t, configuredReviewers)

	_, err := conductor.Review(context.Background(), ReviewSelection{
		Repository: "/repository-must-not-be-resolved",
		Subject:    WorkingChanges(),
		Profile:    "security",
		Reviewer:   "copilot",
	})
	var disabled DisabledReviewerError
	if !errors.As(err, &disabled) {
		t.Fatalf("error = %v, want DisabledReviewerError", err)
	}
}

func TestRepositoryDefaultReviewerOwnsExplicitModelValidation(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {
    "grok": {"enabled": true, "model": "grok-4.5", "allowed_models": ["grok-4.5"]},
    "opencode": {"enabled": true, "model": "model-m", "allowed_models": ["model-m"]}
  }
}`
	conductor := configuredTestConductor(t, configuration)
	executor := successfulExecutor(cleanReview)
	registration := conductor.reviewers.registrations["opencode"]
	registration.executor = executor
	conductor.reviewers.registrations["opencode"] = registration
	repository := changedTestRepository(t)
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`)

	record, err := conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: WorkingChanges(), Model: "model-m"})
	if err != nil {
		t.Fatal(err)
	}
	if record.ProfileRevision.ReviewerID != "opencode" || executor.attemptCount() != 1 {
		t.Fatalf("revision = %#v, attempts = %d", record.ProfileRevision, executor.attemptCount())
	}
}

func TestRepositoryEffectiveReviewerOverridesPersonalPolicy(t *testing.T) {
	conductor := configuredTestConductor(t, `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"},
  "reviewers": {
    "grok": {"enabled": true, "model": "personal-grok"},
    "opencode": {"enabled": true, "model": "personal-opencode"}
  }
}`)
	repository := changedTestRepository(t)
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "defaults": {"reviewer": "grok"},
  "reviewers": {"grok": {"model": "repository-grok"}}
}`)

	explanation, err := conductor.ExplainForRepository(context.Background(), ProfileSelection{Profile: "bugs"}, repository)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.ReviewerID != "grok" || explanation.ProfileRevision.Model != "repository-grok" {
		t.Fatalf("profile reviewer = %q/%q, want grok/repository-grok", explanation.ProfileRevision.ReviewerID, explanation.ProfileRevision.Model)
	}
}

func TestInvalidDefaultUsesAuthoredDefaultPath(t *testing.T) {
	effective := configuration.Effective{
		DefaultReviewer: configuration.Value[string]{Value: "grok", Authored: true, Path: "repository/default.json"},
		Reviewers: map[string]configuration.ReviewerSettings{
			"grok": {
				Enabled: configuration.Value[bool]{Value: false, Authored: true, Path: "repository/grok.json"},
			},
		},
	}

	assertInvalidConfigurationPath(t, effective, "repository/default.json")
}

func TestInvalidReviewerPolicyUsesAuthoredReviewerPath(t *testing.T) {
	effective := configuration.Effective{
		DefaultReviewer: configuration.Value[string]{Value: "grok", Authored: true, Path: "personal/default.json"},
		Reviewers: map[string]configuration.ReviewerSettings{
			"opencode": {
				Model:         configuration.Value[string]{Value: "bad-model", Authored: true, Path: "personal/opencode-model.json"},
				AllowedModels: configuration.Value[[]string]{Value: []string{"allowed-model"}, Authored: true, Path: "repository/opencode-allowed-models.json"},
			},
		},
	}

	assertInvalidConfigurationPath(t, effective, "personal/opencode-model.json")
}

func TestInvalidReviewerPolicyUsesAllowedModelsPathWhenModelUnset(t *testing.T) {
	effective := configuration.Effective{
		Reviewers: map[string]configuration.ReviewerSettings{
			"grok": {
				AllowedModels: configuration.Value[[]string]{Value: []string{"allowed-model"}, Authored: true, Path: "repository/grok-allowed-models.json"},
			},
		},
	}

	assertInvalidConfigurationPath(t, effective, "repository/grok-allowed-models.json")
}

func TestInvalidReviewerPolicyDoesNotInventDefaultPathProvenance(t *testing.T) {
	effective := configuration.Effective{
		DefaultReviewer: configuration.Value[string]{Value: "grok", Authored: true, Path: "repository/default.json"},
		Reviewers: map[string]configuration.ReviewerSettings{
			"grok": {
				AllowedModels: configuration.Value[[]string]{Value: []string{"allowed-model"}, Authored: true},
			},
		},
	}

	assertInvalidConfigurationPath(t, effective, "")
}

func assertInvalidConfigurationPath(t *testing.T, effective configuration.Effective, expected string) {
	t.Helper()
	_, err := configureReviewerCatalog(defaultReviewerCatalog(), effective)
	var invalid InvalidConfigurationError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidConfigurationError", err)
	}
	if invalid.Path != expected {
		t.Fatalf("error path = %q, want %q", invalid.Path, expected)
	}
}

func TestOpenCodeModelOverrideMustBeAllowed(t *testing.T) {
	conductor := configuredTestConductor(t, configuredReviewers)

	explanation, err := conductor.Explain(context.Background(), ProfileSelection{
		Profile:  "bugs",
		Reviewer: "opencode",
		Model:    "opencode-go/deepseek-v4-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Model != "opencode-go/deepseek-v4-flash" {
		t.Fatalf("model = %q", explanation.ProfileRevision.Model)
	}

	_, err = conductor.Explain(context.Background(), ProfileSelection{
		Profile:  "bugs",
		Reviewer: "opencode",
		Model:    "unapproved/model",
	})
	var disallowed ReviewerModelNotAllowedError
	if !errors.As(err, &disallowed) {
		t.Fatalf("error = %v, want ReviewerModelNotAllowedError", err)
	}
}

func TestExplainRejectsUnsupportedExplicitEffort(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	conductor, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]ProfileSelection{
		"copilot auto model": {Profile: "bugs", Reviewer: "copilot", Effort: "high"},
		"opencode auto effort": {
			Profile:  "bugs",
			Reviewer: "opencode",
			Model:    "meta/muse-spark-1.2-contributor",
			Effort:   "auto",
		},
	}
	for name, selection := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := conductor.Explain(context.Background(), selection)
			var unsupported ReviewerEffortNotSupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error = %v, want ReviewerEffortNotSupportedError", err)
			}
			if unsupported.Reviewer != selection.Reviewer || unsupported.Effort != selection.Effort {
				t.Fatalf("error = %#v, want reviewer %q and effort %q", unsupported, selection.Reviewer, selection.Effort)
			}
		})
	}
}

func TestOmittedModelAllowlistAllowsExplicitModel(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {"opencode": {"enabled": true}}
}`
	conductor := configuredTestConductor(t, configuration)

	explanation, err := conductor.Explain(context.Background(), ProfileSelection{
		Profile:  "bugs",
		Reviewer: "opencode",
		Model:    "caller-selected/model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Model != "caller-selected/model" {
		t.Fatalf("model = %q", explanation.ProfileRevision.Model)
	}
}

func TestExplicitEmptyModelAllowlistRejectsConfiguredModel(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {
    "opencode": {
      "enabled": true,
      "model": "meta/muse-spark-1.2-contributor",
      "allowed_models": []
    }
  }
}`
	_, err := invalidConfigurationFromPayload(t, configuration)
	if !strings.Contains(err.Error(), `reviewer "opencode" model "meta/muse-spark-1.2-contributor"`) || !strings.Contains(err.Error(), `is not in allowed_models []`) {
		t.Fatalf("reason = %q", err.Error())
	}
}

func TestExplicitEmptyModelAllowlistRejectsExplicitModel(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {"opencode": {"enabled": true, "allowed_models": []}}
}`
	conductor := configuredTestConductor(t, configuration)

	_, err := conductor.Explain(context.Background(), ProfileSelection{
		Profile:  "bugs",
		Reviewer: "opencode",
		Model:    "caller-selected/model",
	})
	var disallowed ReviewerModelNotAllowedError
	if !errors.As(err, &disallowed) {
		t.Fatalf("error = %v, want ReviewerModelNotAllowedError", err)
	}
	if disallowed.Model != "caller-selected/model" {
		t.Fatalf("model = %q", disallowed.Model)
	}
	if len(disallowed.Allowed) != 0 {
		t.Fatalf("allowed models = %#v, want empty", disallowed.Allowed)
	}
}

func TestNullModelAllowlistIsInvalid(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {"opencode": {"enabled": true, "allowed_models": null}}
}`
	_, err := invalidConfigurationFromPayload(t, configuration)
	if !strings.Contains(err.Error(), "allowed_models must not be null") {
		t.Fatalf("reason = %q", err.Error())
	}
}

func TestNullReviewerModelIsInvalid(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {"grok": {"model": null}}
}`
	assertInvalidConfigurationReason(t, configuration, "model must not be null")
}

func TestEmptyReviewerModelIsInvalid(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {"grok": {"model": ""}}
}`
	assertInvalidConfigurationReason(t, configuration, "model must not be empty")
}

func TestNullReviewerEnabledIsInvalid(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": null}}
}`
	assertInvalidConfigurationReason(t, configuration, "enabled must not be null")
}

func TestNullDefaultReviewerIsInvalid(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "defaults": {"reviewer": null}
}`
	assertInvalidConfigurationReason(t, configuration, "reviewer must not be null")
}

func TestNullReviewersPolicyIsInvalid(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "reviewers": null
}`
	assertInvalidConfigurationReason(t, configuration, "reviewers must not be null")
}

func TestInvalidUserConfigurationFailsClosed(t *testing.T) {
	invalidConfigurationFromPayload(t, `{"schema_version":1,"unexpected":true}`)
}

func TestSemanticConfigurationErrorNamesSourcePath(t *testing.T) {
	path, err := invalidConfigurationFromPayload(t, `{"schema_version":1,"reviewers":{"unknown":{}}}`)
	var invalid configuration.InvalidDocumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidDocumentError", err)
	}
	if invalid.Path != path {
		t.Fatalf("error path = %q, want %q", invalid.Path, path)
	}
}

func TestProfilesUsesConfiguredDefaultWhenBuiltInDefaultIsDisabled(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"},
  "reviewers": {
    "grok": {"enabled": false},
    "opencode": {"enabled": true, "model": "meta/muse-spark-1.2-contributor"}
  }
}`
	conductor := configuredTestConductor(t, configuration)

	profiles, err := conductor.Profiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 {
		t.Fatalf("profiles = %#v", profiles)
	}
	names := []string{profiles[0].Name, profiles[1].Name, profiles[2].Name}
	if !reflect.DeepEqual(names, SupportedProfiles()) {
		t.Fatalf("profile names = %#v", names)
	}
	for _, profile := range profiles {
		got := []string{profile.DefaultReviewer.ReviewerID, profile.DefaultReviewer.Model}
		want := []string{"opencode", "meta/muse-spark-1.2-contributor"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("default reviewer = %#v, want %#v", got, want)
		}
	}
}

func TestReviewRequiresUsableEffectiveDefault(t *testing.T) {
	tests := map[string]struct {
		configuration string
		reason        string
	}{
		"configured default has no model": {configuration: `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"},
  "reviewers": {"opencode": {"enabled": true}}
}`, reason: `reviewer "opencode" requires a model`},
		"built-in default is disabled": {configuration: `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`, reason: `reviewer "grok" is disabled by personal configuration`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := invalidConfigurationFromPayload(t, test.configuration)
			if !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), test.reason)
			}
		})
	}
}

func TestRepositoryCanRepairUnusablePersonalDefault(t *testing.T) {
	conductor := configuredTestConductor(t, `{
  "schema_version": 1,
  "reviewers": {
    "grok": {"enabled": false},
    "opencode": {"enabled": true, "model": "meta/muse-spark-1.2-contributor"}
  }
}`)
	repository := changedTestRepository(t)
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"}
}`)

	explanation, err := conductor.ExplainForRepository(context.Background(), ProfileSelection{Profile: "bugs"}, repository)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.ReviewerID != "opencode" {
		t.Fatalf("reviewer = %q, want opencode", explanation.ProfileRevision.ReviewerID)
	}
}

func TestDisabledReviewerIgnoresInactiveModelAllowlist(t *testing.T) {
	configuration := `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"},
  "reviewers": {
    "grok": {"enabled": false, "model": "grok-4.5", "allowed_models": ["other"]},
    "opencode": {"enabled": true, "model": "meta/muse-spark-1.2-contributor"}
  }
}`
	conductor := configuredTestConductor(t, configuration)

	explanation, err := conductor.Explain(context.Background(), ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Reviewer.ReviewerID != "opencode" {
		t.Fatalf("reviewer = %#v", explanation.ProfileRevision.Reviewer)
	}
}

func TestMissingUserConfigurationPreservesBuiltInDefault(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	conductor, err := New(Config{
		UserConfigurationPath: filepath.Join(t.TempDir(), "missing.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := conductor.Explain(context.Background(), ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Reviewer.ReviewerID != "grok" {
		t.Fatalf("reviewer = %#v", explanation.ProfileRevision.Reviewer)
	}
}

func TestOpenCodeRequiresCallerOwnedModelWithoutConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	conductor, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = conductor.Explain(context.Background(), ProfileSelection{Profile: "bugs", Reviewer: "opencode"})
	var required ReviewerModelRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("error = %v, want ReviewerModelRequiredError", err)
	}
}

func configuredTestConductor(t *testing.T, configuration string) *Conductor {
	t.Helper()
	conductor, _, err := conductorFromConfiguration(t, configuration)
	if err != nil {
		t.Fatal(err)
	}
	return conductor
}

func conductorFromConfiguration(t *testing.T, configuration string) (*Conductor, string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	ledger, err := newLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	conductor, err := New(Config{UserConfigurationPath: path})
	return conductor, path, err
}

func invalidConfigurationFromPayload(t *testing.T, payload string) (string, error) {
	t.Helper()
	conductor, path, err := conductorFromConfiguration(t, payload)
	if err == nil {
		_, err = conductor.Explain(context.Background(), ProfileSelection{Profile: "bugs"})
	}
	if err == nil {
		t.Fatal("configuration payload was accepted")
	}
	return path, err
}

func assertInvalidConfigurationReason(t *testing.T, payload, reason string) {
	t.Helper()
	_, err := invalidConfigurationFromPayload(t, payload)
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), reason)
	}
}
