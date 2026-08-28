package cache

import "testing"

func TestUserProfileKey(t *testing.T) {
	if UserProfileKey("42") != "profile:42" {
		t.Fatal("unexpected profile key")
	}
}
