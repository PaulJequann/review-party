package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitializationDoesNotCreateProfileMaterial(t *testing.T) {
	repository := testRepository(t)
	if _, err := InitializeReviewParty(ReviewPartyInitialization{Repository: repository}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, ".reviewparty", "profiles")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("initialization created Profile material: %v", err)
	}
}
