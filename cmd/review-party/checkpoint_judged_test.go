package main

import (
	"reflect"
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

const (
	judgedBugs model.ReviewID = "rp_1725192000000_00000000000000e1"
	judgedDocs model.ReviewID = "rp_1725192000000_00000000000000e2"
)

// saveFindings records a completed Review whose Findings fail as given, one
// per failure, each located in one.go.
func (fixture checkpointFixture) saveFindings(id model.ReviewID, profile string, changes []model.ContentChange, failures ...string) {
	fixture.t.Helper()
	record := fixture.reviewRecord(id, profile, model.LifecycleCompleted, changes)
	result := &model.ReviewResult{Status: model.ResultFindings, Summary: "findings", Findings: []model.Finding{}}
	for index, failure := range failures {
		result.Findings = append(result.Findings, model.Finding{
			Ordinal: index + 1, Severity: "medium", Category: "bug", Location: "one.go:3",
			Failure: failure, Evidence: "seen", Fix: "fix it", Test: "test it",
		})
	}
	record.Result = result
	fixture.save(record)
}

func (fixture checkpointFixture) recordVerdicts(id model.ReviewID, judgments string) {
	fixture.t.Helper()
	if result := fixture.runWith(judgments, false, "finding", "record", string(id)); result.exit != 0 {
		fixture.t.Fatalf("finding record %s = %+v", id, result)
	}
}

func judgedRange(fixture checkpointFixture) (string, []model.ContentChange) {
	fixture.declare("pre-push", "--requirement", "judged")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	return head, fixture.rangeChanges(head).Changes
}

func TestCheckpointHookRefusesAPushUntilEveryFindingHasAVerdict(t *testing.T) {
	fixture := newCheckpointFixture(t)
	head, changes := judgedRange(fixture)
	fixture.saveFindings(judgedBugs, "bugs", changes, "nil map write", "unchecked error")
	fixture.saveFindings(judgedDocs, "docs", changes, "stale comment")
	refs := pushLine(head, fixture.base)
	scope := " for " + fixture.base[:12] + ".." + head[:12]

	both := "review-party: pre-push Checkpoint needs a verdict on Findings 1, 2 of " + string(judgedBugs) + scope +
		"; judge: review-party finding record " + string(judgedBugs) + "; 1 more Review needs verdicts\n"
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{exit: 1, stderr: both})

	fixture.recordVerdicts(judgedBugs, "1 accept reachable\n2 defer after the refactor\n")
	docs := "review-party: pre-push Checkpoint needs a verdict on Finding 1 of " + string(judgedDocs) + scope +
		"; judge: review-party finding record " + string(judgedDocs) + "\n"
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{exit: 1, stderr: docs})

	fixture.recordVerdicts(judgedDocs, "1 reject the comment is current\n")
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{})

	fixture.saveFindings(judgedDocs, "docs", changes, "stale comment, reworded")
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{exit: 1, stderr: docs})
}

func TestCheckpointCheckReportsUnjudgedFindings(t *testing.T) {
	fixture := newCheckpointFixture(t)
	head, changes := judgedRange(fixture)
	fixture.saveFindings(judgedBugs, "bugs", changes, "nil map write", "unchecked error")
	fixture.saveFindings(judgedDocs, "docs", changes)
	fixture.recordVerdicts(judgedBugs, "2 accept reachable\n")

	result := fixture.runWith("", false, "checkpoint", "check", "pre-push", "--base", fixture.base, "--repo", fixture.repository, "--format", "json")
	if result.exit != 1 {
		t.Fatalf("check = %+v, want exit 1", result)
	}
	report := decodeCheckpointReport(t, result)
	want := []checkpointProfile{
		{Scope: "repository", Name: "bugs", State: "covered", ReviewIDs: []model.ReviewID{judgedBugs}, Unjudged: []engine.UnjudgedReview{{Review: judgedBugs, Ordinals: []int{1}}}},
		{Scope: "repository", Name: "docs", State: "covered", ReviewIDs: []model.ReviewID{judgedDocs}},
	}
	if report.State != engine.CheckpointUnjudged || !report.Covered {
		t.Fatalf("report = %+v, want unjudged and covered", report)
	}
	if !reflect.DeepEqual(report.Profiles, want) {
		t.Fatalf("profiles = %+v, want %+v", report.Profiles, want)
	}
	if want := "review-party finding record " + string(judgedBugs); report.NextCommand != want {
		t.Fatalf("next command = %q, want %q", report.NextCommand, want)
	}

	text := fixture.runWith("", false, "checkpoint", "check", "pre-push", "--base", fixture.base, "--repo", fixture.repository)
	assertRun(t, text, commandRun{exit: 1, stdout: "pre-push checkpoint: " + fixture.base + ".." + head + " (range from --base)\n" +
		"bugs (repository): covered by " + string(judgedBugs) + "; no verdict on Finding 1 of " + string(judgedBugs) + "\n" +
		"docs (repository): covered by " + string(judgedDocs) + "\n" +
		"judge: review-party finding record " + string(judgedBugs) + "\n"})
}

func TestCheckpointWaiverPassesAnUnjudgedPush(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push", "--requirement", "judged", "--waivers", "anyone")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	changes := fixture.rangeChanges(head).Changes
	fixture.saveFindings(judgedBugs, "bugs", changes, "nil map write")
	fixture.saveFindings(judgedDocs, "docs", changes)
	refs := pushLine(head, fixture.base)

	refused := fixture.hook(refs, "pre-push", "origin", "url")
	assertRunContains(t, refused, commandRun{exit: 1, stderr: "; judge: review-party finding record " + string(judgedBugs) + "; or waive: review-party checkpoint waive pre-push"})
	assertRunContains(t, fixture.waive("", false, "pre-push", "--base", fixture.base, "--reason", "hotfix"), commandRun{stdout: "waived by"})
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{})
}

func TestAgentHookRefusesAnUnjudgedPushInEachAgentsForm(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.git("update-ref", "refs/remotes/origin/main", fixture.base)
	head, changes := judgedRange(fixture)
	fixture.saveFindings(judgedBugs, "bugs", changes)
	fixture.saveFindings(judgedDocs, "docs", changes, "stale comment")

	refusal := denial("pre-push Checkpoint needs a verdict on Finding 1 of " + string(judgedDocs) + " for " + fixture.base[:12] + ".." + head[:12] +
		"; judge: review-party finding record " + string(judgedDocs)).run()
	assertRun(t, fixture.agentHook(configuration.IntegrationClaudeCode, claudeEvent(t, toolCall{"Bash", "git push", fixture.repository})), refusal)
	assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git push", fixture.repository})), refusal)

	fixture.recordVerdicts(judgedDocs, "1 accept reworded\n")
	assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git push", fixture.repository})), commandRun{})
}

func TestAgentsMDBlockAsksForVerdictsOnlyUnderJudged(t *testing.T) {
	declared := map[configuration.CheckpointName]configuration.Checkpoint{
		configuration.CheckpointPreCommit: {Requirement: configuration.RequirementReviewed},
		configuration.CheckpointPrePush:   {Requirement: configuration.RequirementJudged},
	}
	want := "Before pushing, review the change with `review-party run --base <upstream> --head HEAD` and record a verdict for each Finding with `review-party finding record <id>`; the pre-push Checkpoint refuses a push until a completed Review covers it and every Finding has a verdict.\n" +
		preCommitLine + helpLine
	if got := agentsMDBody(declared, configuration.SortedCheckpointNames(declared)); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}
