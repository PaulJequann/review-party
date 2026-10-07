package subject

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const zero = "0000000000000000000000000000000000000000"

func commitFile(t *testing.T, repository, name string) string {
	t.Helper()
	writeTestFile(t, filepath.Join(repository, name), "package demo // "+name+"\n")
	runTestCommand(t, repository, "git", "add", name)
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "add "+name)
	return gitText(t, repository, "rev-parse", "HEAD")
}

func TestParsePushedRefs(t *testing.T) {
	input := "refs/heads/main 1111 refs/heads/main 2222\n\n(delete) " + zero + " refs/heads/old 3333\n"
	refs, err := ParsePushedRefs("upstream", []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []PushedRef{
		{Remote: "upstream", LocalRef: "refs/heads/main", LocalObject: "1111", RemoteRef: "refs/heads/main", RemoteObject: "2222"},
		{Remote: "upstream", LocalRef: "(delete)", LocalObject: zero, RemoteRef: "refs/heads/old", RemoteObject: "3333"},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %#v", refs)
	}
	if deletes := [2]bool{refs[0].Deletes(), refs[1].Deletes()}; deletes != [2]bool{false, true} {
		t.Fatalf("deletes = %v", deletes)
	}
	if _, err := ParsePushedRefs("origin", []byte("refs/heads/main 1111\n")); err == nil {
		t.Fatal("a short line was accepted")
	}
}

func TestPushedRefBase(t *testing.T) {
	repository := testRepository(t)
	base := gitText(t, repository, "rev-parse", "HEAD")
	published := commitFile(t, repository, "published.go")
	runTestCommand(t, repository, "git", "checkout", "--quiet", "-b", "feature", base)
	head := commitFile(t, repository, "feature.go")

	pushed := func(remote, remoteObject string) PushedRef {
		return PushedRef{Remote: remote, LocalRef: "refs/heads/feature", LocalObject: head, RemoteRef: "refs/heads/feature", RemoteObject: remoteObject}
	}
	if _, err := PushedRefBase(repository, pushed("origin", zero)); !errors.Is(err, ErrNoPushedRefBase) || !strings.Contains(err.Error(), "refs/heads/feature") {
		t.Fatalf("a new ref without a remote HEAD = %v", err)
	}

	// A forced push compares with its merge base, not the commit it replaces.
	assertPushedRefBase(t, repository, pushed("origin", published), base)

	runTestCommand(t, repository, "git", "update-ref", "refs/remotes/origin/HEAD", published)
	assertPushedRefBase(t, repository, pushed("upstream", "9999999999999999999999999999999999999999"), base)

	runTestCommand(t, repository, "git", "update-ref", "refs/remotes/upstream/HEAD", head)
	assertPushedRefBase(t, repository, pushed("upstream", zero), head)
}

func assertPushedRefBase(t *testing.T, repository string, ref PushedRef, want string) {
	t.Helper()
	if got, err := PushedRefBase(repository, ref); err != nil || got != want {
		t.Fatalf("base for remote %s object %s = %q, %v, want %s", ref.Remote, ref.RemoteObject, got, err, want)
	}
}

func TestResolveHookLocations(t *testing.T) {
	repository := testRepository(t)
	locations, err := ResolveHookLocations(repository)
	if err != nil || locations != (HookLocations{Directory: filepath.Join(repository, ".git", "hooks"), Common: filepath.Join(repository, ".git")}) {
		t.Fatalf("plain hooks = %#v, %v", locations, err)
	}

	for _, hooksPath := range []string{".githooks", " spaced hooks "} {
		runTestCommand(t, repository, "git", "config", "core.hooksPath", hooksPath)
		locations, err = ResolveHookLocations(repository)
		if err != nil || locations != (HookLocations{Directory: filepath.Join(repository, hooksPath), HooksPath: hooksPath, Common: filepath.Join(repository, ".git")}) {
			t.Fatalf("core.hooksPath %q = %#v, %v", hooksPath, locations, err)
		}
	}

	worktree := filepath.Join(t.TempDir(), "linked")
	runTestCommand(t, repository, "git", "config", "--unset", "core.hooksPath")
	runTestCommand(t, repository, "git", "worktree", "add", "--quiet", "-b", "linked", worktree)
	locations, err = ResolveHookLocations(worktree)
	if err != nil || locations != (HookLocations{Directory: filepath.Join(repository, ".git", "hooks"), Common: filepath.Join(repository, ".git")}) {
		t.Fatalf("linked worktree hooks = %#v, %v", locations, err)
	}
}
