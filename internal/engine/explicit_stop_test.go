package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reviewparty/internal/model"
)

// A hard error after the explicit Review started must not leave its record
// running: status and wait would report a live run that no process owns.
func TestExplicitReviewHardErrorPersistsATerminalRecord(t *testing.T) {
	repository := changedTestRepository(t)
	writeExecutableProfile(t, repository, "local-docs")
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	artifactRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifactRoot, "artifacts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	conductor.getRunner().publisher = newArtifactPublisher(mustNewArtifactStore(t, artifactRoot))

	returned, err := conductor.ReviewExplicitProfile(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "repository:local-docs"})
	if err == nil {
		t.Fatal("explicit review succeeded, want the artifact publish failure")
	}
	stored, loadErr := conductor.store.Load(returned.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.Termination == nil {
		t.Fatalf("stored record %q has no termination", stored.Lifecycle)
	}
	if stored.Lifecycle != model.LifecycleIncomplete || returned.Lifecycle != stored.Lifecycle {
		t.Fatalf("stored = %q, returned = %q; want both incomplete", stored.Lifecycle, returned.Lifecycle)
	}
}
