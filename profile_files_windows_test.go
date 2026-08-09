//go:build windows

package reviewparty

import "testing"

func TestPackagedProfileUsesEmbedPathOnWindows(t *testing.T) {
	profile, err := (profileLibrary{}).findProfile(profileLookup{name: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.source != "packaged:profiles/bugs.md" {
		t.Fatalf("source = %q", profile.source)
	}
}
