//go:build windows

package engine

import "testing"

func TestSavedProfileUsesPortableSourceOnWindows(t *testing.T) {
	profile, err := newTestProfileLibrary(t).findProfile(profileLookup{name: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.source != "global:profiles/bugs" {
		t.Fatalf("source = %q", profile.source)
	}
}
