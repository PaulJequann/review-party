package configurationhub

import (
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
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

func TestSnapshotWarnsAboutSkippedTemplates(t *testing.T) {
	const want = "Template skill:empty skipped: /x/SKILL.md has no instructions after its frontmatter"
	for _, test := range []struct {
		name    string
		skipped []configuration.SkippedTemplate
		want    []string
	}{
		{name: "skipped", skipped: []configuration.SkippedTemplate{{TemplateID: "skill:empty", Path: "/x/SKILL.md", Reason: "/x/SKILL.md has no instructions after its frontmatter"}}, want: []string{want}},
		{name: "none"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := configuration.NewManager(configuration.Options{
				GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
				SkippedTemplates: test.skipped,
			})
			snapshot, err := buildSnapshot(manager, configuration.Repository(t.TempDir()))
			if err != nil {
				t.Fatalf("buildSnapshot: %v", err)
			}
			if !slices.Equal(snapshot.Warnings, test.want) {
				t.Fatalf("warnings = %#v, want %#v", snapshot.Warnings, test.want)
			}
		})
	}
}
