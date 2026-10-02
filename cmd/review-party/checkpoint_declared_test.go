package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

type commandRun struct {
	exit   int
	stdout string
	stderr string
}

// runWith runs one command with the given standard input, as a terminal
// session when terminal is set.
func (fixture checkpointFixture) runWith(input string, terminal bool, arguments ...string) commandRun {
	fixture.t.Helper()
	var stdout, stderr bytes.Buffer
	streams := productionCommandIO(strings.NewReader(input), &stdout, &stderr)
	streams.terminal = func(any) bool { return terminal }
	exit := execute(context.Background(), arguments, streams)
	return commandRun{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

func (fixture checkpointFixture) declare(arguments ...string) {
	fixture.t.Helper()
	set := append([]string{"config", "checkpoint", "set"}, arguments...)
	if result := fixture.runWith("", false, append(set, "--repo", fixture.repository, "--yes")...); result.exit != 0 {
		fixture.t.Fatalf("declare %v = %+v", arguments, result)
	}
}

func (fixture checkpointFixture) waive(input string, terminal bool, arguments ...string) commandRun {
	fixture.t.Helper()
	return fixture.runWith(input, terminal, append([]string{"checkpoint", "waive"}, append(arguments, "--repo", fixture.repository)...)...)
}

func assertRun(t *testing.T, got, want commandRun) {
	t.Helper()
	if got != want {
		t.Fatalf("run =\n%+v\nwant\n%+v", got, want)
	}
}

// assertRunContains fails unless the run exits with want's code and each
// stream contains want's fragment for it.
func assertRunContains(t *testing.T, got, want commandRun) {
	t.Helper()
	seen := commandRun{exit: got.exit, stdout: fragmentOf(got.stdout, want.stdout), stderr: fragmentOf(got.stderr, want.stderr)}
	if seen != want {
		t.Fatalf("run =\n%+v\nwant exit %d with fragments\n%+v", got, want.exit, want)
	}
}

func fragmentOf(text, fragment string) string {
	if strings.Contains(text, fragment) {
		return fragment
	}
	return text
}

// decodeCheckpointReport reads the JSON report a command printed, which must
// be all of its standard output.
func decodeCheckpointReport(t *testing.T, result commandRun) checkpointReport {
	t.Helper()
	var report checkpointReport
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatalf("decode %+v: %v", result, err)
	}
	return report
}

func TestCheckpointSetPublishesTheDeclaration(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--exempt", "*.md", "--exempt", "docs/**", "--small-change-lines", "3", "--waivers", "anyone", "--integration", "git")

	payload, err := os.ReadFile(filepath.Join(fixture.repository, ".reviewparty", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Checkpoints map[string]any `json:"checkpoints"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"pre-push": map[string]any{
		"requirement": "reviewed", "exempt_paths": []any{"*.md", "docs/**"}, "small_change_lines": float64(3),
		"waivers": "anyone", "integrations": []any{"git"},
	}}
	if !reflect.DeepEqual(config.Checkpoints, want) {
		t.Fatalf("checkpoints = %#v", config.Checkpoints)
	}
	result := fixture.runWith("", false, "config", "checkpoint", "set", "pre-push", "--exempt", "/abs", "--repo", fixture.repository, "--yes")
	assertRunContains(t, result, commandRun{exit: 1, stderr: `pattern "/abs" must be relative`})
	assertRunContains(t, fixture.runWith("", false, "config", "checkpoint", "remove", "pre-push", "--repo", fixture.repository, "--yes"), commandRun{})
	again := fixture.runWith("", false, "checkpoint", "waive", "pre-push", "--reason", "x", "--base", fixture.base, "--repo", fixture.repository)
	assertRunContains(t, again, commandRun{exit: usageExitCode, stderr: "is not declared"})
}

func TestCheckpointDocsOnlyPushIsExempt(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--exempt", "*.md", "--exempt", "docs/**")
	fixture.commit("README.md", "# app\n")
	head := fixture.commit("docs/guide.txt", "guide\n")

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base)
	want := "pre-push checkpoint: " + fixture.base + ".." + head + " (range from --base)\n" +
		"exempt paths: README.md, docs/guide.txt\n" +
		"exempt: every changed path is exempt\n"
	assertRun(t, commandRun{exit, stdout, stderr}, commandRun{stdout: want})
}

func TestCheckpointExemptPathsLeaveBothSides(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--exempt", "*.md")
	code := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	reviewed := fixture.rangeChanges(code).Changes
	fixture.saveReview("rp_1725192000000_00000000000000b1", "bugs", model.LifecycleCompleted, reviewed)
	fixture.saveReview("rp_1725192000000_00000000000000b2", "docs", model.LifecycleCompleted, reviewed)
	fixture.commit("NOTES.md", "notes\n")

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base, "--format", "json")
	report := decodeCheckpointReport(t, commandRun{exit, stdout, stderr})
	if report.Exemption == nil {
		t.Fatalf("report = %+v", report)
	}
	got := [3]string{string(report.State), strings.Join(report.Exemption.ExemptPaths, ","), strconv.Itoa(exit)}
	if want := [3]string{"covered", "NOTES.md", "0"}; got != want {
		t.Fatalf("state, exempt paths, exit = %v, want %v", got, want)
	}
}

func TestCheckpointSmallChangePassesAndLargeChangeNamesTheLimit(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--small-change-lines", "2")
	small := fixture.commit("app.go", "package app\n\nconst one = 1\n")

	exit, stdout, _ := fixture.check("pre-push", "--base", fixture.base)
	if exit != 0 || !strings.Contains(stdout, "exempt: small change of 2 lines, at most 2 pass\n") {
		t.Fatalf("small change exit = %d, stdout = %q", exit, stdout)
	}

	fixture.commit("app.go", "package app\n\nconst one = 1\nconst two = 2\n")
	exit, stdout, _ = fixture.check("pre-push", "--base", fixture.base)
	if exit != 1 || !strings.Contains(stdout, "changed lines: 3, over the small-change limit of 2\n") {
		t.Fatalf("large change from %s exit = %d, stdout = %q", small, exit, stdout)
	}
}

func TestCheckpointWaiverPolicies(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	rangeArgs := []string{"pre-push", "--base", fixture.base}
	waive := func(input string, terminal bool, extra ...string) commandRun {
		return fixture.waive(input, terminal, append(append([]string{}, rangeArgs...), extra...)...)
	}
	person := "a person must run review-party checkpoint waive pre-push in a terminal"

	assertRunContains(t, waive("", false, "--reason", "  "), commandRun{exit: usageExitCode, stderr: "--reason is required"})
	assertRunContains(t, waive("", false, "--reason", "hotfix"), commandRun{exit: usageExitCode, stderr: "config checkpoint set pre-push"})

	fixture.declare("pre-push", "--waivers", "none")
	assertRunContains(t, waive("y\n", true, "--reason", "hotfix"), commandRun{exit: usageExitCode, stderr: "waivers policy"})

	fixture.declare("pre-push", "--waivers", "human")
	assertRunContains(t, waive("", false, "--reason", "hotfix"), commandRun{exit: usageExitCode, stderr: person})
	assertRunContains(t, waive("", false, "--reason", "hotfix", "--yes"), commandRun{exit: usageExitCode, stderr: person})
	assertRunContains(t, waive("n\n", true, "--reason", "hotfix", "--yes"), commandRun{exit: 1, stderr: "waiver cancelled"})

	confirmed := waive("y\n", true, "--reason", "hotfix", "--format", "json")
	assertRunContains(t, confirmed, commandRun{stdout: confirmed.stdout, stderr: "Waive the pre-push Checkpoint for this exact change"})
	report := decodeCheckpointReport(t, confirmed)
	if report.Waiver == nil {
		t.Fatalf("confirmed human waiver = %+v", confirmed)
	}
	got := [3]string{string(report.State), string(report.Waiver.WaivedBy), report.Waiver.Reason}
	if want := [3]string{"waived", string(model.WaivedByTerminal), "hotfix"}; got != want {
		t.Fatalf("state, waived by, reason = %v, want %v", got, want)
	}

	exit, stdout, stderr := fixture.check(rangeArgs...)
	assertRunContains(t, commandRun{exit, stdout, stderr}, commandRun{stdout: "waived by " + string(report.Waiver.ID) + " (terminal): hotfix\n"})
}

func TestCheckpointAnyonePolicyWaivesWithoutATerminalAndOffersTheCommand(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--waivers", "anyone")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")

	exit, stdout, _ := fixture.check("pre-push", "--base", fixture.base)
	waiveLine := "waive: review-party checkpoint waive pre-push --base " + fixture.base + " --head " + head + " --repo " + shellQuoteArgument(fixture.repository) + ` --reason "<why>"` + "\n"
	if exit != 1 || !strings.HasSuffix(stdout, waiveLine) {
		t.Fatalf("uncovered anyone check exit = %d, stdout = %q", exit, stdout)
	}

	result := fixture.waive("", false, "pre-push", "--base", fixture.base, "--reason", "generated code")
	if result.exit != 0 || !strings.Contains(result.stdout, "(non-interactive): generated code\n") {
		t.Fatalf("anyone waiver = %+v", result)
	}

	fixture.commit("one.go", "package app\n\nconst one = 2\n")
	if exit, stdout, _ := fixture.check("pre-push", "--base", fixture.base); exit != 1 || strings.Contains(stdout, "waived by") {
		t.Fatalf("a changed range kept the waiver: exit = %d, stdout = %q", exit, stdout)
	}
}
