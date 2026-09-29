package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillTemplatesComeOnlyFromCallerHome(t *testing.T) {
	home, repository := t.TempDir(), t.TempDir()
	writeSkill(t, filepath.Join(home, ".agents", "skills", "caller"), "Caller skill.")
	writeSkill(t, filepath.Join(repository, ".agents", "skills", "planted"), "Planted by the Subject author.")
	writeSkill(t, filepath.Join(repository, ".claude", "skills", "planted-claude"), "Planted by the Subject author.")
	t.Setenv("HOME", home)
	t.Chdir(repository)

	var ids []string
	for _, template := range reviewPartyConfigurationOptions().Templates {
		ids = append(ids, template.ID)
	}
	var caller bool
	for _, id := range ids {
		switch id {
		case "skill:caller":
			caller = true
		case "skill:planted", "skill:planted-claude":
			t.Fatalf("repository skill %s became a Template: %v", id, ids)
		}
	}
	if !caller {
		t.Fatalf("caller skill missing from Templates: %v", ids)
	}
}

func writeSkill(t *testing.T, directory, body string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "SKILL.md"), body)
}
