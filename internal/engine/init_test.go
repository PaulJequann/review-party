package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializationDoesNotCreateProfileMaterial(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repository := testRepository(t)
	if _, err := InitializeReviewParty(ReviewPartyInitialization{Repository: repository}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, ".reviewparty", "profiles")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("initialization created Profile material: %v", err)
	}
}

func TestInitializationRejectsCombinedRecoveryActions(t *testing.T) {
	_, err := InitializeReviewParty(ReviewPartyInitialization{BackupIncompatible: true, Fresh: true})
	if err == nil || !strings.Contains(err.Error(), "separate requests") {
		t.Fatalf("combined recovery error = %v", err)
	}
}

func TestStateDirectoryIsRequiredWithoutAHomeOrConfiguration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	_, err := New(Config{UserConfigurationPath: filepath.Join(t.TempDir(), "config.toml")})
	if !errors.Is(err, errStateDirectoryRequired) {
		t.Fatalf("New without a state directory = %v, want %v", err, errStateDirectoryRequired)
	}
}
