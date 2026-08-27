package engine

import (
	"context"
	"testing"
	"time"
)

func TestProfilesListOnlySavedExecutableProfiles(t *testing.T) {
	conductor := testConductor(t, successfulExecutor(cleanReview), time.Second)
	profiles, err := conductor.ProfilesForRepository(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 {
		t.Fatalf("profiles = %#v", profiles)
	}
	for _, profile := range profiles {
		if profile.Source == "packaged" || profile.Error != "" {
			t.Fatalf("profile = %#v", profile)
		}
	}
}
