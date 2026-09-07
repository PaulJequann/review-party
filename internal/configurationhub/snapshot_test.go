package configurationhub

import (
	"strings"
	"testing"
)

func TestPluralizedCountUsesSingularOnlyForOne(t *testing.T) {
	for _, test := range []struct {
		count int
		want  string
	}{
		{count: 0, want: "0 Profiles"},
		{count: 1, want: "1 Profile"},
		{count: 2, want: "2 Profiles"},
	} {
		if got := pluralizedCount("Profile", test.count); got != test.want {
			t.Fatalf("pluralizedCount(Profile, %d) = %q, want %q", test.count, got, test.want)
		}
	}
	if got := pluralizedCount("Party", 1); got != "1 Party" {
		t.Fatalf("pluralizedCount(Party, 1) = %q, want %q", got, "1 Party")
	}
	if strings.Contains(pluralizedCount("Party", 0), "Partys") {
		t.Fatalf("pluralizedCount(Party, 0) = %q, want regular plural", pluralizedCount("Party", 0))
	}
}
