package subject

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func blobOf(t *testing.T, repository string, content []byte) string {
	t.Helper()
	output, err := gitInputOutput(repository, content, "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func TestMissingObjectsNamesOnlyTheObjectsTheRepositoryLacks(t *testing.T) {
	repository := testRepository(t)
	kept := blobOf(t, repository, []byte("kept\n"))
	pruned := strings.Repeat("ab", 20)

	missing, err := MissingObjects(repository, slices.Values([]string{kept, pruned}))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]bool{pruned: true}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
	if missing, err := MissingObjects(repository, slices.Values([]string{})); err != nil || len(missing) != 0 {
		t.Fatalf("missing of nothing = %v, %v", missing, err)
	}
}

func TestMeasureDeltaCountsLinesBetweenBlobsPerPath(t *testing.T) {
	repository := testRepository(t)
	delta := []model.ContentChange{
		{Path: "pkg/sub/edited.go", Before: blobOf(t, repository, []byte("a\nb\nc\n")), After: blobOf(t, repository, []byte("a\nx\nc\nd\n"))},
		{Path: "added.go", Before: model.ZeroObjectID, After: blobOf(t, repository, []byte("one\ntwo\n"))},
		{Path: "removed.go", Before: blobOf(t, repository, []byte("1\n2\n3\n")), After: model.ZeroObjectID},
		{Path: "image.bin", Before: model.ZeroObjectID, After: blobOf(t, repository, []byte{0, 1, 2, 0, 3})},
	}

	lines, err := MeasureDelta(repository, delta)
	if err != nil {
		t.Fatal(err)
	}
	want := DeltaLines{ByPath: map[string]int{"pkg/sub/edited.go": 3, "added.go": 2, "removed.go": 3}, Binary: []string{"image.bin"}, Total: 8}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("delta lines = %#v, want %#v", lines, want)
	}
	if !lines.Exceeds(100) {
		t.Fatal("a binary path should exceed any allowance")
	}
	text := DeltaLines{ByPath: map[string]int{"a.go": 8}, Total: 8}
	if text.Exceeds(8) || !text.Exceeds(7) {
		t.Fatal("Exceeds should compare the total with the allowance")
	}
}

func TestMeasureDeltaOfNothingIsZero(t *testing.T) {
	lines, err := MeasureDelta(testRepository(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, DeltaLines{ByPath: map[string]int{}}) || lines.Exceeds(0) {
		t.Fatalf("empty delta = %#v", lines)
	}
}

func TestMeasureDeltaNamesAMissingBlob(t *testing.T) {
	repository := testRepository(t)
	delta := []model.ContentChange{{Path: "lost.go", Before: model.ZeroObjectID, After: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	_, err := MeasureDelta(repository, delta)
	if err == nil || !strings.Contains(err.Error(), "lost.go") {
		t.Fatalf("error = %v, want one naming lost.go", err)
	}
}

// A nested repository's commits are never objects of this repository, so its
// gitlink is carried, measured, and printed by its pointer alone.
func TestAGitlinkIsMeasuredWithoutItsCommits(t *testing.T) {
	repository := testRepository(t)
	nested := filepath.Join(repository, "nested")
	runTestCommand(t, repository, "git", "init", "--quiet", "nested")
	runTestCommand(t, nested, "git", "config", "user.email", "review-party@example.invalid")
	runTestCommand(t, nested, "git", "config", "user.name", "Review Party Test")
	runTestCommand(t, nested, "git", "commit", "--quiet", "--allow-empty", "-m", "first")
	first := gitText(t, nested, "rev-parse", "HEAD")
	assertContentChanges(t, repository, []model.ContentChange{{Path: "nested", Before: model.ZeroObjectID, After: first, AfterGitlink: true}})
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "add nested")

	runTestCommand(t, nested, "git", "commit", "--quiet", "--allow-empty", "-m", "second")
	second := gitText(t, nested, "rev-parse", "HEAD")
	bumped := []model.ContentChange{{Path: "nested", Before: first, After: second, BeforeGitlink: true, AfterGitlink: true}}
	assertContentChanges(t, repository, bumped)
	lines, err := MeasureDelta(repository, bumped)
	if err != nil || !reflect.DeepEqual(lines, DeltaLines{ByPath: map[string]int{"nested": 2}, Total: 2}) {
		t.Fatalf("gitlink delta lines = %#v, %v", lines, err)
	}
	patch, err := DeltaPatch(repository, bumped)
	if err != nil || !strings.Contains(string(patch), "-Subproject commit "+first+"\n+Subproject commit "+second+"\n") {
		t.Fatalf("gitlink patch = %q, %v", patch, err)
	}
}

// A path that turns between a file and a nested repository keeps each side's
// kind, so the file's lines count in full.
func TestATypeChangeMeasuresTheFileSideInFull(t *testing.T) {
	repository := testRepository(t)
	file := blobOf(t, repository, []byte("1\n2\n3\n4\n5\n"))
	pointer := strings.Repeat("ab", 20)
	for _, change := range []model.ContentChange{
		{Path: "vendored", Before: file, After: pointer, AfterGitlink: true},
		{Path: "vendored", Before: pointer, After: file, BeforeGitlink: true},
	} {
		lines, err := MeasureDelta(repository, []model.ContentChange{change})
		if err != nil || lines.Total != 6 {
			t.Fatalf("type change %+v lines = %#v, %v, want 6", change, lines, err)
		}
		patch, err := DeltaPatch(repository, []model.ContentChange{change})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Subproject commit " + pointer, "5\n"} {
			if !strings.Contains(string(patch), want) {
				t.Fatalf("type change patch lacks %q:\n%s", want, patch)
			}
		}
	}
}

func TestADeltaPathMayHoldANewline(t *testing.T) {
	repository := testRepository(t)
	delta := []model.ContentChange{{Path: "odd\nname.go", Before: blobOf(t, repository, []byte("a\n")), After: blobOf(t, repository, []byte("b\n"))}}
	lines, err := MeasureDelta(repository, delta)
	if err != nil || !reflect.DeepEqual(lines, DeltaLines{ByPath: map[string]int{"odd\nname.go": 2}, Total: 2}) {
		t.Fatalf("newline path lines = %#v, %v", lines, err)
	}
	if _, err := DeltaPatch(repository, delta); err != nil {
		t.Fatal(err)
	}
}

// assertContentChanges checks the working tree and, once staged, the index
// both carry want.
func assertContentChanges(t *testing.T, repository string, want []model.ContentChange) {
	t.Helper()
	working, err := WorkingContentChanges(repository, AllFiles)
	if err != nil || !reflect.DeepEqual(working, want) {
		t.Fatalf("working changes = %#v, %v, want %#v", working, err, want)
	}
	runTestCommand(t, repository, "git", "add", "-A")
	staged, err := StagedContentChanges(repository)
	if err != nil || !reflect.DeepEqual(staged, want) {
		t.Fatalf("staged changes = %#v, %v, want %#v", staged, err, want)
	}
}

func TestDeltaPatchDiffsTheBlobsUnderTheirPaths(t *testing.T) {
	repository := testRepository(t)
	delta := []model.ContentChange{{Path: "pkg/edited.go", Before: blobOf(t, repository, []byte("a\nb\n")), After: blobOf(t, repository, []byte("a\nc\n"))}}
	patch, err := DeltaPatch(repository, delta)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"diff --git a/pkg/edited.go b/pkg/edited.go", "-b\n", "+c\n"} {
		if !strings.Contains(string(patch), want) {
			t.Fatalf("patch lacks %q:\n%s", want, patch)
		}
	}
}

func TestWorkingContentChangesPersistTheHashedObjects(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"edited\"\n")
	writeTestFile(t, filepath.Join(repository, "fresh.txt"), "fresh content\n")
	changes, err := WorkingContentChanges(repository, AllFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	for _, change := range changes {
		if _, err := gitOutput(repository, "cat-file", "-e", change.After); err != nil {
			t.Fatalf("working blob of %s is not in the object database: %v", change.Path, err)
		}
	}
}
