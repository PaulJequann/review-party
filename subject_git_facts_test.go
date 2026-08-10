package reviewparty

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkingChangesRecordsTrackedAndUntrackedSubjectFacts(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n")
	writeTestFile(t, filepath.Join(repository, "new.go"), "package demo\n\nconst added = true\n")
	if err := os.WriteFile(filepath.Join(repository, "image.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}

	subject, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	want := SubjectFacts{ChangedFiles: 3, Additions: 4, Deletions: 1, BinaryFiles: 1}
	if !reflect.DeepEqual(subject.Facts, &want) {
		t.Fatalf("facts = %#v, want %#v", subject.Facts, want)
	}
}
