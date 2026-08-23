package configuration

import (
	"errors"
	"path/filepath"
	"testing"
)

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
