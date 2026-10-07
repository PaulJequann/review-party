package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

func boundedCheckpoint(lines, budget int) configuration.Checkpoint {
	checkpoint := configuration.NewCheckpoint()
	checkpoint.UnreviewedLines, checkpoint.ReviewBudget = lines, budget
	return checkpoint
}

// writeBoundedSelection declares the Profiles and the same Checkpoint at both
// pre-commit and pre-push, so a working-changes run and the later committed
// range are bounded alike.
func writeBoundedSelection(t *testing.T, repository string, checkpoint configuration.Checkpoint, profiles ...string) {
	t.Helper()
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Repository: []configuration.SelectionItem{}}
	for _, profile := range profiles {
		selection.Global = append(selection.Global, configuration.SelectionItem{Profile: profile})
	}
	document := map[string]any{
		"schema_version": 1,
		"reviews":        selection,
		"checkpoints":    map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPreCommit: checkpoint, configuration.CheckpointPrePush: checkpoint},
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, ".reviewparty", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

// runOneMember runs the saved selection and returns its one member's record.
func runOneMember(t *testing.T, conductor *Conductor, selection model.RunSelection) model.ReviewRecord {
	t.Helper()
	bundle, err := conductor.Run(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Members) != 1 {
		t.Fatalf("members = %d, want 1", len(bundle.Members))
	}
	record, err := conductor.Inspect(context.Background(), bundle.Members[0].ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func changeOf(t *testing.T, changes []model.ContentChange, path string) model.ContentChange {
	t.Helper()
	for _, change := range changes {
		if change.Path == path {
			return change
		}
	}
	t.Fatalf("content changes %#v do not include %s", changes, path)
	return model.ContentChange{}
}

func TestRunUnreviewedReviewsOnlyTheDeltaAndExtendsTheChain(t *testing.T) {
	repository := changedTestRepository(t)
	writeBoundedSelection(t, repository, boundedCheckpoint(0, 3), "bugs")
	executor := successfulExecutor(findingsReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	warnings := &warningLog{}
	conductor.runner.warn = warnings.add
	working := model.RunSelection{Repository: repository, Subject: model.WorkingChanges()}

	first := runOneMember(t, conductor, working)
	reviewed := changeOf(t, first.Subject.ContentChanges, "review.go")
	if first.Subject.Kind != model.SubjectWorkingChanges || len(first.Subject.ContentChanges) != 2 {
		t.Fatalf("first subject = %s with %d content changes, want working-changes with review.go and the untracked config", first.Subject.Kind, len(first.Subject.ContentChanges))
	}
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n\nvar next = 1\n")
	second := runOneMember(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Unreviewed: true})
	if second.Subject.Kind != model.SubjectUnreviewedDelta {
		t.Fatalf("second subject kind = %s, want %s", second.Subject.Kind, model.SubjectUnreviewedDelta)
	}
	current := strings.TrimSpace(runTestCommandOutput(t, repository, "git", "hash-object", "review.go"))
	if want := []model.ContentChange{{Path: "review.go", Before: reviewed.After, After: current}}; !slices.Equal(second.Subject.ContentChanges, want) {
		t.Fatalf("delta edges = %#v, want %#v from the first Review's reviewed state", second.Subject.ContentChanges, want)
	}
	if !strings.Contains(second.Subject.Patch, "+var next = 1") || strings.Contains(second.Subject.Patch, "+const state") {
		t.Fatalf("delta patch reviews more than the unreviewed lines:\n%s", second.Subject.Patch)
	}
	prompt := executor.attempts[1].Prompt
	for _, want := range []string{"claim to verify", "- " + string(first.ID) + " #1 HIGH review.go:3: The changed state is not handled."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("delta prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(executor.attempts[0].Prompt, "claim to verify") {
		t.Fatal("the plain Review's prompt carries the delta framing")
	}
	_, err := conductor.Run(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Unreviewed: true})
	if !errors.Is(err, ErrNothingUnreviewed) {
		t.Fatalf("third run error = %v, want ErrNothingUnreviewed", err)
	}
	if executor.attemptCount() != 2 {
		t.Fatalf("attempts = %d, want no Review when nothing is unreviewed", executor.attemptCount())
	}
	if want := []string{"global:bugs: Review 1 of 3", "global:bugs: Review 2 of 3", "global:bugs: nothing unreviewed; skipped"}; !slices.Equal(warnings.all(), want) {
		t.Fatalf("warnings = %q, want %q", warnings.all(), want)
	}

	base := strings.TrimSpace(runTestCommandOutput(t, repository, "git", "rev-parse", "HEAD"))
	runTestCommand(t, repository, "git", "add", "review.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: fixed after review")
	content, err := subject.CommittedRangeContentChanges(repository, model.CommittedRange(base, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	report := checkPrePush(t, conductor, repository, content.Changes)
	if report.State != CheckpointCovered {
		t.Fatalf("pre-push state = %s, want covered by the working and delta Reviews", report.State)
	}
	if got, want := report.Coverage.Profiles[0].Reviews, []model.ReviewID{first.ID, second.ID}; !slices.Equal(got, want) {
		t.Fatalf("chain evidence = %v, want %v", got, want)
	}
}

func TestRunUnreviewedWithNothingReviewedKeepsThePlainSubject(t *testing.T) {
	repository := changedTestRepository(t)
	writeBoundedSelection(t, repository, boundedCheckpoint(0, 3), "bugs")
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})

	record := runOneMember(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Unreviewed: true})
	if record.Subject.Kind != model.SubjectWorkingChanges {
		t.Fatalf("subject kind = %s, want the plain working-changes Subject when nothing was reviewed", record.Subject.Kind)
	}
	if strings.Contains(executor.attempts[0].Prompt, "claim to verify") {
		t.Fatal("a first Review's prompt carries the delta framing")
	}
}

func TestRunUnreviewedLeavesExemptPathsOut(t *testing.T) {
	repository := changedTestRepository(t)
	checkpoint := boundedCheckpoint(0, 3)
	checkpoint.ExemptPaths = []string{"notes.md"}
	writeBoundedSelection(t, repository, checkpoint, "bugs")
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	runOneMember(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n\nvar next = 1\n")
	writeTestFile(t, filepath.Join(repository, "notes.md"), "scratch notes\n")
	record := runOneMember(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Unreviewed: true})
	if got := changedPaths(record.Subject.ContentChanges); !slices.Equal(got, []string{"review.go"}) {
		t.Fatalf("delta paths = %q, want review.go without the exempt notes.md", got)
	}
	if strings.Contains(record.Subject.Patch, "notes.md") {
		t.Fatalf("delta patch reviews the exempt path:\n%s", record.Subject.Patch)
	}
}

func TestRunUnreviewedFramesOnlyTheProfilesWithEarlierReviews(t *testing.T) {
	repository := changedTestRepository(t)
	writeBoundedSelection(t, repository, boundedCheckpoint(0, 3), "bugs")
	executor := successfulExecutor(findingsReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	first := runOneMember(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	writeBoundedSelection(t, repository, boundedCheckpoint(0, 3), "bugs", "code-quality")
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n\nvar next = 1\n")
	bundle, err := conductor.Run(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Unreviewed: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Members) != 2 || executor.attemptCount() != 3 {
		t.Fatalf("members = %d, attempts = %d, want both Profiles reviewed", len(bundle.Members), executor.attemptCount())
	}
	framed := framedPrompts(executor.attempts[1:])
	finding := "- " + string(first.ID) + " #1 HIGH review.go:3: The changed state is not handled."
	if len(framed) != 1 || !strings.Contains(framed[0], finding) {
		t.Fatalf("framed prompts = %d, want only the bugs Profile framed with its prior Finding %q", len(framed), finding)
	}
}

func framedPrompts(attempts []attemptSpec) []string {
	var framed []string
	for _, attempt := range attempts {
		if strings.Contains(attempt.Prompt, "claim to verify") {
			framed = append(framed, attempt.Prompt)
		}
	}
	return framed
}
