package settings

import (
	"bytes"
	"testing"
)

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	if !Defaults().EmailNotifications {
		t.Fatal("email notifications should default on")
	}
	var payload bytes.Buffer
	if err := Save(&payload, Settings{EmailNotifications: true}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(&payload)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.EmailNotifications {
		t.Fatal("enabled preference was not restored")
	}
}
