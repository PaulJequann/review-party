package subject

import (
	"errors"
	"path/filepath"
	"reflect"
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
	if err != nil || lines.Total != 0 || len(lines.Binary) != 0 || lines.Exceeds(0) {
		t.Fatalf("empty delta = %#v, %v", lines, err)
	}
}

func TestMeasureDeltaNamesAMissingObject(t *testing.T) {
	repository := testRepository(t)
	delta := []model.ContentChange{{Path: "lost.go", Before: model.ZeroObjectID, After: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	_, err := MeasureDelta(repository, delta)
	if !errors.Is(err, ErrMissingObject) || !strings.Contains(err.Error(), "lost.go") {
		t.Fatalf("error = %v, want ErrMissingObject naming lost.go", err)
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
