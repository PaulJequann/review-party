package subject

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPushCommandRefsForTheCurrentBranch(t *testing.T) {
	repository := testRepository(t)
	branch := gitText(t, repository, "symbolic-ref", "--short", "HEAD")
	base := gitText(t, repository, "rev-parse", "HEAD")
	head := commitFile(t, repository, "feature.go")
	current := func(remote, remoteRef, remoteObject string) []PushedRef {
		return []PushedRef{{Remote: remote, LocalRef: "refs/heads/" + branch, LocalObject: head, RemoteRef: remoteRef, RemoteObject: remoteObject}}
	}

	assertPushCommandRefs(t, repository, PushCommand{}, current("origin", "refs/heads/"+branch, zero))

	runTestCommand(t, repository, "git", "config", "branch."+branch+".remote", "upstream")
	runTestCommand(t, repository, "git", "config", "branch."+branch+".merge", "refs/heads/trunk")
	runTestCommand(t, repository, "git", "update-ref", "refs/remotes/upstream/trunk", base)
	assertPushCommandRefs(t, repository, PushCommand{}, current("upstream", "refs/heads/trunk", base))
	assertPushCommandRefs(t, repository, PushCommand{Remote: "upstream"}, current("upstream", "refs/heads/trunk", base))
	assertPushCommandRefs(t, repository, PushCommand{Remote: "fork"}, current("fork", "refs/heads/"+branch, zero))

	runTestCommand(t, repository, "git", "config", "push.default", "current")
	assertPushCommandRefs(t, repository, PushCommand{}, current("upstream", "refs/heads/"+branch, zero))
	runTestCommand(t, repository, "git", "config", "--unset", "push.default")

	runTestCommand(t, repository, "git", "config", "remote.pushDefault", "fork")
	assertPushCommandRefs(t, repository, PushCommand{}, current("fork", "refs/heads/"+branch, zero))
	runTestCommand(t, repository, "git", "config", "branch."+branch+".pushRemote", "mirror")
	assertPushCommandRefs(t, repository, PushCommand{}, current("mirror", "refs/heads/"+branch, zero))
	assertPushCommandRefs(t, repository, PushCommand{Remote: "upstream"}, current("upstream", "refs/heads/trunk", base))

	for key, value := range map[string]string{"push.default": "matching", "remote.mirror.push": "refs/heads/*:refs/heads/*"} {
		runTestCommand(t, repository, "git", "config", key, value)
		if refs, err := (PushCommand{}).Refs(repository); err == nil {
			t.Fatalf("push with %s = %s resolved %v", key, value, refs)
		}
		runTestCommand(t, repository, "git", "config", "--unset", key)
	}

	runTestCommand(t, repository, "git", "checkout", "--quiet", "--detach")
	if _, err := (PushCommand{}).Refs(repository); !errors.Is(err, ErrDetachedPush) {
		t.Fatalf("detached push = %v", err)
	}
}

func TestPushCommandRefsForRefspecs(t *testing.T) {
	repository := testRepository(t)
	branch := gitText(t, repository, "symbolic-ref", "--short", "HEAD")
	base := gitText(t, repository, "rev-parse", "HEAD")
	runTestCommand(t, repository, "git", "tag", "--annotate", "-m", "release", "v1")
	head := commitFile(t, repository, "feature.go")
	runTestCommand(t, repository, "git", "update-ref", "refs/remotes/origin/release", base)

	command := PushCommand{Remote: "origin", Refspecs: []string{"+HEAD:release", ":old", "v1", base + ":refs/heads/pinned", branch}}
	want := []PushedRef{
		{Remote: "origin", LocalRef: "refs/heads/" + branch, LocalObject: head, RemoteRef: "refs/heads/release", RemoteObject: base},
		{Remote: "origin", LocalRef: "refs/tags/v1", LocalObject: base, RemoteRef: "refs/tags/v1", RemoteObject: zero},
		{Remote: "origin", LocalRef: "", LocalObject: base, RemoteRef: "refs/heads/pinned", RemoteObject: zero},
		{Remote: "origin", LocalRef: "refs/heads/" + branch, LocalObject: head, RemoteRef: "refs/heads/" + branch, RemoteObject: zero},
	}
	assertPushCommandRefs(t, repository, command, want)

	for refspec, reason := range map[string]string{
		"missing":                     `cannot resolve pushed revision "missing"`,
		base:                          "names no destination ref",
		"refs/heads/*:refs/heads/*":   "cannot check pattern refspec",
		"refs/heads/*:refs/heads/x/*": "cannot check pattern refspec",
	} {
		if _, err := (PushCommand{Remote: "origin", Refspecs: []string{refspec}}).Refs(repository); err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("refspec %q = %v, want %q", refspec, err, reason)
		}
	}
}

func assertPushCommandRefs(t *testing.T, repository string, command PushCommand, want []PushedRef) {
	t.Helper()
	refs, err := command.Refs(repository)
	if err != nil || !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs for %#v = %#v, %v\nwant %#v", command, refs, err, want)
	}
}

func TestTrackedWorkingChangesLeaveOutUntrackedFiles(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n")
	writeTestFile(t, filepath.Join(repository, "untracked.go"), "package demo\n")

	changes, err := WorkingContentChanges(repository, TrackedFiles)
	if err != nil || !reflect.DeepEqual(contentChangePaths(changes), []string{"review.go"}) {
		t.Fatalf("tracked changes = %#v, %v", changes, err)
	}
}
