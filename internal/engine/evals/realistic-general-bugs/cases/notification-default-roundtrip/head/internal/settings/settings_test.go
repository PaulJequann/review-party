package settings

import (
	"bytes"
	"testing"
)

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	defaults := Defaults()
	if !defaults.EmailNotifications || defaults.DigestHour != 9 {
		t.Fatalf("defaults = %#v", defaults)
	}
	var payload bytes.Buffer
	if err := Save(&payload, Settings{EmailNotifications: true, DigestHour: 14}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(&payload)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.EmailNotifications || loaded.DigestHour != 14 {
		t.Fatalf("loaded = %#v", loaded)
	}
}
