package reviewparty

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const configuredReviewers = `{
  "version": 1,
  "default_reviewer": "opencode",
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
		Profile:    "bugs",
		Reviewer:   "copilot",
	})
	var disabled DisabledReviewerError
	if !errors.As(err, &disabled) {
		t.Fatalf("error = %v, want DisabledReviewerError", err)
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

func TestOmittedModelAllowlistAllowsExplicitModel(t *testing.T) {
	configuration := `{
  "version": 1,
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
  "version": 1,
  "reviewers": {
    "opencode": {
      "enabled": true,
      "model": "meta/muse-spark-1.2-contributor",
      "allowed_models": []
    }
  }
}`
	invalid, _ := invalidConfigurationFromPayload(t, configuration)
	if !strings.Contains(invalid.Reason, `model "meta/muse-spark-1.2-contributor" is not allowed for reviewer "opencode"`) {
		t.Fatalf("reason = %q", invalid.Reason)
	}
}

func TestExplicitEmptyModelAllowlistRejectsExplicitModel(t *testing.T) {
	configuration := `{
  "version": 1,
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
  "version": 1,
  "reviewers": {"opencode": {"enabled": true, "allowed_models": null}}
}`
	invalid, _ := invalidConfigurationFromPayload(t, configuration)
	if !strings.Contains(invalid.Reason, "allowed_models must be an array, not null") {
		t.Fatalf("reason = %q", invalid.Reason)
	}
}

func TestInvalidUserConfigurationFailsClosed(t *testing.T) {
	invalidConfigurationFromPayload(t, `{"version":1,"unexpected":true}`)
}

func TestSemanticConfigurationErrorNamesSourcePath(t *testing.T) {
	invalid, path := invalidConfigurationFromPayload(t, `{"version":1,"reviewers":{"unknown":{}}}`)
	if invalid.Path != path {
		t.Fatalf("error path = %q, want %q", invalid.Path, path)
	}
}

func TestProfilesUsesConfiguredDefaultWhenBuiltInDefaultIsDisabled(t *testing.T) {
	configuration := `{
  "version": 1,
  "default_reviewer": "opencode",
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
	if len(profiles) != 2 {
		t.Fatalf("profiles = %#v", profiles)
	}
	names := []string{profiles[0].Name, profiles[1].Name}
	if !reflect.DeepEqual(names, []string{"bugs", "documentation"}) {
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

func TestConfigurationRequiresUsableEffectiveDefault(t *testing.T) {
	tests := map[string]struct {
		configuration string
		reason        string
	}{
		"configured default has no model": {configuration: `{
  "version": 1,
  "default_reviewer": "opencode",
  "reviewers": {"opencode": {"enabled": true}}
}`, reason: `effective default reviewer "opencode" requires a model`},
		"built-in default is disabled": {configuration: `{
  "version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`, reason: `effective default reviewer "grok" is disabled`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			invalid, _ := invalidConfigurationFromPayload(t, test.configuration)
			if !strings.Contains(invalid.Reason, test.reason) {
				t.Fatalf("reason = %q, want it to contain %q", invalid.Reason, test.reason)
			}
		})
	}
}

func TestDisabledReviewerIgnoresInactiveModelAllowlist(t *testing.T) {
	configuration := `{
  "version": 1,
  "default_reviewer": "opencode",
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
	conductor, err := New(Config{
		RecordDirectory:       t.TempDir(),
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
	conductor, err := New(Config{RecordDirectory: t.TempDir()})
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
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	conductor, err := New(Config{RecordDirectory: t.TempDir(), UserConfigurationPath: path})
	if err != nil {
		t.Fatal(err)
	}
	return conductor
}

func invalidConfigurationFromPayload(t *testing.T, payload string) (InvalidUserConfigurationError, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{RecordDirectory: t.TempDir(), UserConfigurationPath: path})
	var invalid InvalidUserConfigurationError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidUserConfigurationError", err)
	}
	return invalid, path
}
