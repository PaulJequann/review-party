package engine

import "testing"

func TestProfilesListOnlySavedExecutableProfiles(t *testing.T) {
	library := newTestProfileLibrary(t)
	profiles, err := library.list("")
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
