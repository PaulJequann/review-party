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
	fixture.declare("pre-push", "--exempt", "*.md", "--exempt", "docs/**", "--unreviewed-lines", "3", "--review-budget", "2", "--waivers", "anyone", "--integration", "git")

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
		"requirement": "reviewed", "exempt_paths": []any{"*.md", "docs/**"}, "unreviewed_lines": float64(3), "review_budget": float64(2),
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

func TestCheckpointAllowanceAndBudgetDecideAfterAReview(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--unreviewed-lines", "1", "--review-budget", "1")
	reviewed := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	exit, stdout, _ := fixture.check("pre-push", "--base", fixture.base)
	if exit != 1 || !strings.Contains(stdout, "bugs (repository): missing, 3 unreviewed lines in one.go; 0 of 1 Reviews spent\n") {
		t.Fatalf("unreviewed exit = %d, stdout = %q", exit, stdout)
	}

	changes := fixture.rangeChanges(reviewed).Changes
	fixture.saveReview(judgedBugs, "bugs", model.LifecycleCompleted, changes)
	fixture.saveReview(judgedDocs, "docs", model.LifecycleCompleted, changes)
	fixture.commit("one.go", "package app\n\nconst one = 1\nconst two = 2\n")
	exit, stdout, _ = fixture.check("pre-push", "--base", fixture.base)
	if exit != 0 || !strings.Contains(stdout, "bugs (repository): residual, 1 unreviewed line in one.go; 1 of 1 Reviews spent; after "+string(judgedBugs)+"\n") {
		t.Fatalf("residual exit = %d, stdout = %q", exit, stdout)
	}

	fixture.commit("one.go", "package app\n\nconst one = 1\nconst two = 2\nconst three = 3\n")
	exit, stdout, _ = fixture.check("pre-push", "--base", fixture.base)
	if exit != 1 || !strings.HasSuffix(stdout, "stop: 2 unreviewed lines exceed 1 after 1 of 1 Reviews; ask a person\n") || strings.Contains(stdout, "review-party run") {
		t.Fatalf("spent exit = %d, stdout = %q", exit, stdout)
	}
}

func TestCheckpointFallsBackWhenAReviewedBlobIsGone(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--unreviewed-lines", "1", "--review-budget", "3")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	changes := fixture.rangeChanges(head).Changes
	pruned := []model.ContentChange{{Path: "one.go", Before: changes[0].Before, After: strings.Repeat("ab", 20)}}
	fixture.saveReview("rp_1725192000000_00000000000000e5", "bugs", model.LifecycleCompleted, pruned)
	fixture.saveReview("rp_1725192000000_00000000000000e6", "docs", model.LifecycleCompleted, changes)

	got := fixture.runWith("", false, "checkpoint", "check", "pre-push", "--base", fixture.base, "--repo", fixture.repository)
	assertRunContains(t, got, commandRun{exit: 1, stdout: "bugs (repository): missing, 3 unreviewed lines in one.go; 1 of 3 Reviews spent\n"})
	if got.stderr != "" {
		t.Fatalf("pruned stderr = %q", got.stderr)
	}
}

// TestCheckpointCountsAGitlinkLikeAnyPath bumps a nested repository whose
// commits this repository never holds, so the gitlink is measured, reviewed,
// and credited by its pointer alone.
func TestCheckpointCountsAGitlinkLikeAnyPath(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--unreviewed-lines", "0", "--review-budget", "3")
	fixture.declare("pre-commit", "--unreviewed-lines", "0", "--review-budget", "3")
	nested, added := fixture.commitNestedRepository("nested")
	fixture.saveReview("rp_1725192000000_00000000000000f1", "bugs", model.LifecycleCompleted, fixture.rangeChanges(added).Changes)
	fixture.saveReview("rp_1725192000000_00000000000000f2", "docs", model.LifecycleCompleted, fixture.rangeChanges(added).Changes)
	bumped := nested.commitBump()

	got := fixture.runWith("", false, "checkpoint", "check", "pre-push", "--base", fixture.base, "--repo", fixture.repository)
	assertRunContains(t, got, commandRun{exit: 1, stdout: "bugs (repository): missing, 2 unreviewed lines in nested; 1 of 3 Reviews spent; after rp_1725192000000_00000000000000f1\n"})
	if got.stderr != "" {
		t.Fatalf("bump stderr = %q", got.stderr)
	}

	fixture.saveReview("rp_1725192000000_00000000000000f3", "bugs", model.LifecycleCompleted, fixture.rangeChanges(bumped).Changes)
	fixture.saveReview("rp_1725192000000_00000000000000f4", "docs", model.LifecycleCompleted, fixture.rangeChanges(bumped).Changes)
	if exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base); exit != 0 {
		t.Fatalf("reviewed bump exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}

	nested.stageBump()
	got = fixture.runWith("", false, "checkpoint", "check", "pre-commit", "--repo", fixture.repository)
	assertRunContains(t, got, commandRun{exit: 1, stdout: "bugs (repository): missing, 2 unreviewed lines in nested; 0 of 3 Reviews spent\n"})
}

// TestCheckpointOffersTheWaiveCommandOnlyOnASpentBudget declares anyone may
// waive, then checks a missing Review and a spent budget on the same range.
func TestCheckpointOffersTheWaiveCommandOnlyOnASpentBudget(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--unreviewed-lines", "1", "--review-budget", "1", "--waivers", "anyone")
	reviewed := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	rangeArgs := []string{"pre-push", "--base", fixture.base}

	exit, stdout, _ := fixture.check(rangeArgs...)
	if exit != 1 || !strings.HasSuffix(stdout, "next: review-party run --unreviewed --base "+fixture.base+" --head "+reviewed+" --repo "+shellQuoteArgument(fixture.repository)+"\n") {
		t.Fatalf("missing exit = %d, stdout = %q", exit, stdout)
	}
	if report := decodeCheckpointReport(t, fixture.runWith("", false, append([]string{"checkpoint", "check"}, append(rangeArgs, "--format", "json", "--repo", fixture.repository)...)...)); report.WaiveCommand != "" {
		t.Fatalf("missing report offers %q", report.WaiveCommand)
	}

	changes := fixture.rangeChanges(reviewed).Changes
	fixture.saveReview(judgedBugs, "bugs", model.LifecycleCompleted, changes)
	fixture.saveReview(judgedDocs, "docs", model.LifecycleCompleted, changes)
	head := fixture.commit("one.go", "package app\n\nconst one = 1\nconst two = 2\nconst three = 3\n")
	waiveCommand := "review-party checkpoint waive pre-push --base " + fixture.base + " --head " + head + " --repo " + shellQuoteArgument(fixture.repository) + ` --reason "<why>"`

	exit, stdout, _ = fixture.check(rangeArgs...)
	if exit != 1 || !strings.HasSuffix(stdout, "stop: 2 unreviewed lines exceed 1 after 1 of 1 Reviews; ask a person\nwaive: "+waiveCommand+"\n") {
		t.Fatalf("spent exit = %d, stdout = %q", exit, stdout)
	}
	if report := decodeCheckpointReport(t, fixture.runWith("", false, append([]string{"checkpoint", "check"}, append(rangeArgs, "--format", "json", "--repo", fixture.repository)...)...)); report.WaiveCommand != waiveCommand {
		t.Fatalf("spent report offers %q, want %q", report.WaiveCommand, waiveCommand)
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

func TestCheckpointHumanWaiverInJSONPromptsAPersonWhileStdoutIsPiped(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	fixture.declare("pre-push", "--waivers", "human")
	var stdout, stderr bytes.Buffer
	streams := productionCommandIO(strings.NewReader("y\n"), &stdout, &stderr)
	streams.terminal = func(stream any) bool { return stream != &stdout }
	arguments := []string{"checkpoint", "waive", "pre-push", "--base", fixture.base, "--reason", "hotfix", "--format", "json", "--repo", fixture.repository}
	result := commandRun{exit: execute(context.Background(), arguments, streams), stdout: stdout.String(), stderr: stderr.String()}
	assertRunContains(t, result, commandRun{stdout: result.stdout, stderr: "Waive the pre-push Checkpoint for this exact change"})
	if report := decodeCheckpointReport(t, result); report.Waiver == nil || report.Waiver.WaivedBy != model.WaivedByTerminal {
		t.Fatalf("piped human waiver = %+v", result)
	}
}

func TestCheckpointAnyonePolicyWaivesWithoutATerminal(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--waivers", "anyone")
	fixture.commit("one.go", "package app\n\nconst one = 1\n")

	result := fixture.waive("", false, "pre-push", "--base", fixture.base, "--reason", "generated code")
	if result.exit != 0 || !strings.Contains(result.stdout, "(non-interactive): generated code\n") {
		t.Fatalf("anyone waiver = %+v", result)
	}

	fixture.commit("one.go", "package app\n\nconst one = 2\n")
	if exit, stdout, _ := fixture.check("pre-push", "--base", fixture.base); exit != 1 || strings.Contains(stdout, "waived by") {
		t.Fatalf("a changed range kept the waiver: exit = %d, stdout = %q", exit, stdout)
	}
}
