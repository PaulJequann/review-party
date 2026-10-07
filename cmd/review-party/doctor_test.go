package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func (fixture checkpointFixture) doctor(arguments ...string) commandRun {
	fixture.t.Helper()
	return fixture.runWith("", false, append([]string{"doctor", "--repo", fixture.repository}, arguments...)...)
}

func (fixture checkpointFixture) installIntegration(integration string) {
	fixture.t.Helper()
	if result := fixture.runWith("", false, "checkpoint", "install", integration, "--repo", fixture.repository, "--yes"); result.exit != 0 {
		fixture.t.Fatalf("install %s = %+v", integration, result)
	}
}

// assertRunMatching is assertRun with want.stdout read as a regular
// expression that must match all of the output.
func assertRunMatching(t *testing.T, got, want commandRun) {
	t.Helper()
	if regexp.MustCompile(`^(?:` + want.stdout + `)$`).MatchString(got.stdout) {
		got.stdout = want.stdout
	}
	assertRun(t, got, want)
}

func decodeDoctor(t *testing.T, result commandRun, exit int) doctorResult {
	t.Helper()
	if result.exit != exit {
		t.Fatalf("json doctor = %+v, want exit %d", result, exit)
	}
	var report doctorResult
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatalf("json doctor = %+v: %v", result, err)
	}
	return report
}

func TestDoctorNamesEachMissingFloorIntegrationWithItsFix(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--integration", "git", "--integration", "claude-code", "--integration", "agents-md")
	fixture.installIntegration("git")
	repository := shellWord(fixture.repository)

	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\n" +
		"Checkpoint pre-push has no claude-code hook; fix: review-party checkpoint install claude-code --repo " + repository + "\n" +
		"Checkpoint pre-push has no agents-md block; fix: review-party checkpoint install agents-md --repo " + repository + "\n"})

	directory := t.TempDir()
	t.Chdir(directory)
	config := filepath.Join(directory, "review-party.json")
	assertRunContains(t, fixture.doctor("--config", "review-party.json"), commandRun{stdout: "fix: review-party checkpoint install agents-md --repo " + repository + " --config " + shellQuoteArgument(config) + "\n"})

	result := fixture.doctor("--format", "json")
	install := "review-party checkpoint install %s --repo " + repository
	want := []floorGap{
		{Checkpoint: "pre-push", Integration: "claude-code", State: gapMissing, Path: filepath.Join(fixture.repository, ".claude", "settings.json"), Tool: hookToolClaude, Fix: fmt.Sprintf(install, "claude-code")},
		{Checkpoint: "pre-push", Integration: "agents-md", State: gapMissing, Path: filepath.Join(fixture.repository, "AGENTS.md"), Tool: hookToolAgentsMD, Fix: fmt.Sprintf(install, "agents-md")},
	}
	if gaps := decodeDoctor(t, result, 0).IntegrationGaps; !reflect.DeepEqual(gaps, want) {
		t.Fatalf("integration_gaps = %+v, want %+v", gaps, want)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.stdout), &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"unresolved_names", "exemption_conflicts", "recent_waivers", "template_drift"} {
		if string(fields[field]) != "[]" {
			t.Fatalf("%s = %s, want []", field, fields[field])
		}
	}
}

func TestDoctorNamesAnInactiveAndAnEditedHook(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--integration", "git")
	if err := os.Mkdir(filepath.Join(fixture.repository, ".husky"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.installIntegration("git")
	hook := filepath.Join(fixture.repository, ".husky", "pre-push")

	inactive := "Checkpoint pre-push git hook does not run until husky is active in this clone; fix: npx husky\n"
	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\n" + inactive})

	content, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	fixture.writeFile(".husky/pre-push", strings.Replace(string(content), "then exit 1; fi", "then exit 0; fi", 1))
	edited := "Checkpoint pre-push git hook in " + hook + " was edited, and install leaves it alone until the edited one is removed; fix: review-party checkpoint install git --repo " + shellWord(fixture.repository) + "\n"
	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\n" + edited + inactive})
}

func TestDoctorReportsAStaleAgentsMDBlock(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--integration", "agents-md")
	fixture.installIntegration("agents-md")
	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\n"})

	fixture.declare("pre-commit", "--integration", "agents-md")
	path := filepath.Join(fixture.repository, "AGENTS.md")
	stale := " agents-md block in " + path + " no longer matches the declared Checkpoints; fix: review-party checkpoint install agents-md --repo " + shellWord(fixture.repository) + "\n"
	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\nCheckpoint pre-push" + stale + "Checkpoint pre-commit" + stale})
}

func TestDoctorNamesUnresolvedNamesAndStillFailsTheConfiguration(t *testing.T) {
	fixture := newCheckpointFixture(t)
	requireConfigSuccess(t, []string{
		"config", "profile", "create", "bugs", "--template", "bugs", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes",
	})
	requireConfigSuccess(t, []string{"config", "reviews", "add", "--scope", "global", "--profile", "bugs", "--repo", fixture.repository, "--yes"})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	create := "review-party config profile create bugs --scope global --template bugs" + profileExecutionPlaceholders
	assertRun(t, fixture.doctor(), commandRun{
		stdout: "Missing Global Profile bugs, selected by reviews.global[0]; fix: " + create + "\n",
		stderr: "review-party: profile \"bugs\" was not found in global Configuration, selected by reviews.global[0]\n",
		exit:   1,
	})

	want := []unresolvedName{{Kind: configuration.ItemProfile, Name: "bugs", Scope: configuration.ScopeGlobal, SelectedBy: "reviews.global[0]", Fix: create}}
	if names := decodeDoctor(t, fixture.doctor("--format", "json"), 1).UnresolvedNames; !reflect.DeepEqual(names, want) {
		t.Fatalf("unresolved_names = %+v, want %+v", names, want)
	}
}

func TestDoctorNamesAMarkdownExemptionADocumentationProfileReviews(t *testing.T) {
	fixture := newCheckpointFixture(t)
	requireConfigSuccess(t, []string{
		"config", "profile", "create", "prose", "--template", "documentation", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes",
	})
	requireConfigSuccess(t, []string{"config", "reviews", "add", "--scope", "global", "--profile", "prose", "--repo", fixture.repository, "--yes"})
	fixture.declare("pre-push", "--requirement", "judged", "--exempt", "*.md", "--exempt", "vendor/**", "--exempt", "docs/**", "--unreviewed-lines", "5", "--review-budget", "2", "--waivers", "anyone")
	repository := shellWord(fixture.repository)

	fix := "review-party config checkpoint set pre-push --repo " + repository + " --requirement judged --exempt 'vendor/**' --unreviewed-lines 5 --review-budget 2 --waivers anyone"
	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\nCheckpoint pre-push exempts *.md, docs/**, so changes the documentation Profile prose reviews pass it unreviewed; fix: " + fix + "\n"})

	fixture.declare("pre-push", "--requirement", "judged", "--exempt", "vendor/**", "--unreviewed-lines", "5", "--review-budget", "2", "--waivers", "anyone")
	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\n"})
}

func TestDoctorListsRecentWaiversReadOnly(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--waivers", "anyone")
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	if result := fixture.waive("", false, "pre-push", "--base", fixture.base, "--reason", "generated\r\ncode\n"); result.exit != 0 {
		t.Fatalf("waive = %+v", result)
	}

	assertRunMatching(t, fixture.doctor(), commandRun{stdout: `configuration is valid\nWaiver cw_\S+ waived pre-push on \d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ \(non-interactive\): generated code\n`})

	other := newCheckpointFixture(t)
	t.Setenv("XDG_STATE_HOME", filepath.Dir(fixture.ledger))
	assertRun(t, other.doctor(), commandRun{stdout: "configuration is valid\n"})
}

func TestDoctorNamesALedgerThatNeedsPreparation(t *testing.T) {
	fixture := newCheckpointFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(fixture.ledger, "ledger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = (SELECT MAX(version) FROM schema_migrations)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	fix := "; fix: review-party init --repo " + shellWord(fixture.repository) + "\n"
	assertRunMatching(t, fixture.doctor(), commandRun{stdout: `configuration is valid\nRecent Waivers unread: the review ledger needs an upgrade before doctor can read it` + regexp.QuoteMeta(fix)})
}

func TestDoctorCreatesNoStateDirectory(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--integration", "git")
	fixture.installIntegration("git")
	state := filepath.Join(t.TempDir(), "absent")
	t.Setenv("XDG_STATE_HOME", state)

	assertRun(t, fixture.doctor(), commandRun{stdout: "configuration is valid\n"})
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("doctor created %s: %v", state, err)
	}
}
