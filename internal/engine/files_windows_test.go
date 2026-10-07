//go:build windows

package engine

import "testing"

func TestSavedProfileUsesPortableSourceOnWindows(t *testing.T) {
	manager := newTestConfigurationManager(t)
	conductor, err := newConductorWithManager(nil, defaultReviewerCatalog(), manager, 0)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := conductor.resolveProfile(profileRequest{name: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.source != "global:profiles/bugs" {
		t.Fatalf("source = %q", profile.source)
	}
}
