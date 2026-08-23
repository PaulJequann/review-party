package configuration

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEffectiveReviewerAccessReturnsDefensiveCopies(t *testing.T) {
	personalRoot := t.TempDir()
	writeDocument(t, filepath.Join(personalRoot, "config.json"), `{
  "schema_version": 1,
  "reviewers": {"opencode": {"model": "model-a", "allowed_models": ["model-a", "model-b"]}}
}`)
	effective, err := testManager(t, personalRoot).Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}

	ids := effective.ReviewerIDs()
	ids[0] = "mutated"
	if reflect.DeepEqual(ids, effective.ReviewerIDs()) {
		t.Fatal("ReviewerIDs returned shared enumeration state")
	}
	policy := mustReviewerPolicy(t, effective, "opencode")
	policy.AllowedModels.Value[0] = "mutated"
	got := mustReviewerPolicy(t, effective, "opencode").AllowedModels.Value
	if !reflect.DeepEqual(got, []string{"model-a", "model-b"}) {
		t.Fatalf("allowed models = %v, want defensive copy", got)
	}
}

func TestEffectiveReviewerWithoutPackagedModelRemainsPresent(t *testing.T) {
	effective, err := testManager(t, t.TempDir()).Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}
	model := mustReviewerPolicy(t, effective, "opencode").Model
	if model.Value != "" {
		t.Fatalf("model value = %q, want empty", model.Value)
	}
	if model.Source != SourcePackaged {
		t.Fatalf("model source = %q, want packaged", model.Source)
	}
	if model.Authored {
		t.Fatal("model is authored, want packaged fallback")
	}
}

func TestEffectiveDisabledReviewerLeavesCrossScopeModelPolicyInert(t *testing.T) {
	personalRoot := t.TempDir()
	repository := t.TempDir()
	personalPath := filepath.Join(personalRoot, "config.json")
	repositoryPath := filepath.Join(repository, ".reviewparty", "config.json")
	writeDocument(t, personalPath, `{
  "schema_version": 1,
  "reviewers": {"opencode": {"enabled": false}}
}`)
	writeDocument(t, repositoryPath, `{
  "schema_version": 1,
  "reviewers": {"opencode": {"model": "model-a", "allowed_models": ["model-b"]}}
}`)

	effective, err := testManager(t, personalRoot).Resolve(Request{Repository: Repository(repository)})
	if err != nil {
		t.Fatal(err)
	}
	policy, exists := effective.ReviewerPolicy("opencode")
	if !exists || policy.Enabled.Value {
		t.Fatalf("policy = %#v, want disabled effective reviewer", policy)
	}
}

func TestEffectiveEnabledReviewerRejectsCrossScopeModelPolicyWithWinningProvenance(t *testing.T) {
	personalRoot := t.TempDir()
	repository := t.TempDir()
	personalPath := filepath.Join(personalRoot, "config.json")
	repositoryPath := filepath.Join(repository, ".reviewparty", "config.json")
	writeDocument(t, personalPath, `{
  "schema_version": 1,
  "reviewers": {"opencode": {"enabled": false, "allowed_models": ["model-b"]}}
}`)
	writeDocument(t, repositoryPath, `{
  "schema_version": 1,
  "reviewers": {"opencode": {"enabled": true, "model": "model-a"}}
}`)

	_, err := testManager(t, personalRoot).Resolve(Request{Repository: Repository(repository)})
	var invalid EffectiveReviewerPolicyError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want EffectiveReviewerPolicyError", err)
	}
	if invalid.Model.Source != SourceRepository || invalid.Model.Path != repositoryPath {
		t.Fatalf("model provenance = %#v", invalid.Model)
	}
	if invalid.AllowedModels.Source != SourcePersonal || invalid.AllowedModels.Path != personalPath {
		t.Fatalf("allowed_models provenance = %#v", invalid.AllowedModels)
	}
}
