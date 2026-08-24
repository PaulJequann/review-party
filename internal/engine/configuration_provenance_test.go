package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
	"testing"
)

func TestDisabledReviewerSelectionPreservesWinningConfigurationProvenance(t *testing.T) {
	conductor := configuredTestConductor(t, `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`)

	_, err := conductor.Explain(context.Background(), model.ProfileSelection{Profile: "bugs", Reviewer: "grok"})
	var disabled DisabledReviewerError
	if !errors.As(err, &disabled) {
		t.Fatalf("error = %v, want DisabledReviewerError", err)
	}
	personalRoot, rootErr := conductor.profiles.manager().PersonalRoot()
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	wantPath := filepath.Join(personalRoot, "config.json")
	if disabled.Source != "personal" || disabled.Path != wantPath {
		t.Fatalf("disabled provenance = %#v, want personal %q", disabled, wantPath)
	}
	if strings.Contains(err.Error(), "user configuration") {
		t.Fatalf("error retains stale wording: %q", err)
	}
}
