package cache

import "testing"

func TestUserProfileKey(t *testing.T) {
	if UserProfileKey("acme", "42") != "profile:acme:42" {
		t.Fatal("unexpected profile key")
	}
}
