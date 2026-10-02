package subject

// A Caller Agent hook runs before git push, so git has not yet told anyone
// which refs it will push. PushCommand rebuilds the pre-push ref lines from
// the command line and the clone's branch and tracking state.

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
)

// PushCommand is what one git push command line names. Remote is empty when
// the command names none; Refspecs is empty when it pushes the current branch.
type PushCommand struct {
	Remote   string
	Refspecs []string
}

var ErrDetachedPush = errors.New("git push without a refspec from a detached HEAD names no branch")

const zeroObject = "0000000000000000000000000000000000000000"

// Refs returns one PushedRef per ref the push would update, leaving out
// deletions. Without refspecs the current branch goes to its upstream when
// the upstream is on the pushed remote, else to the same name on the remote,
// which is origin unless named. A refspec's remote object is the clone's
// tracking ref for the destination, zero when there is none.
func (command PushCommand) Refs(repository string) ([]PushedRef, error) {
	if len(command.Refspecs) == 0 {
		return command.currentBranchRefs(repository)
	}
	var refs []PushedRef
	for _, refspec := range command.Refspecs {
		ref, err := command.refspecRef(repository, strings.TrimPrefix(refspec, "+"))
		if err != nil {
			return nil, err
		}
		if !ref.Deletes() {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (command PushCommand) currentBranchRefs(repository string) ([]PushedRef, error) {
	branch, err := gitLine(repository, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, ErrDetachedPush
	}
	upstreamRemote, err := gitLine(repository, "config", "--default", "", "--get", "branch."+branch+".remote")
	if err != nil {
		return nil, err
	}
	upstreamRef, err := gitLine(repository, "config", "--default", "", "--get", "branch."+branch+".merge")
	if err != nil {
		return nil, err
	}
	remote := cmp.Or(command.Remote, upstreamRemote, "origin")
	destination := "refs/heads/" + branch
	if upstreamRemote == remote && upstreamRef != "" {
		destination = upstreamRef
	}
	object, err := gitLine(repository, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, err
	}
	return []PushedRef{{Remote: remote, LocalRef: "refs/heads/" + branch, LocalObject: object, RemoteRef: destination, RemoteObject: trackingObject(repository, remote, destination)}}, nil
}

// refspecRef resolves one "<src>[:<dst>]" refspec. An empty source deletes
// the destination, which the zero local object records.
func (command PushCommand) refspecRef(repository, refspec string) (PushedRef, error) {
	source, destination, _ := strings.Cut(refspec, ":")
	if strings.Contains(refspec, "*") {
		return PushedRef{}, fmt.Errorf("cannot check pattern refspec %q", refspec)
	}
	if source == "" {
		return PushedRef{Remote: command.Remote, LocalObject: zeroObject, RemoteRef: destination}, nil
	}
	object, err := gitLine(repository, "rev-parse", "--verify", "--quiet", source+"^{commit}")
	if err != nil {
		return PushedRef{}, fmt.Errorf("cannot resolve pushed revision %q", source)
	}
	fullName, err := gitLine(repository, "rev-parse", "--symbolic-full-name", source)
	if err != nil {
		return PushedRef{}, err
	}
	destination, err = pushDestination(source, fullName, destination)
	if err != nil {
		return PushedRef{}, err
	}
	return PushedRef{Remote: command.Remote, LocalRef: fullName, LocalObject: object, RemoteRef: destination, RemoteObject: trackingObject(repository, command.Remote, destination)}, nil
}

// pushDestination qualifies a refspec's destination as git does: a missing
// destination is the source's own ref, and a short one takes the source's
// namespace, tags for a tag and branches otherwise.
func pushDestination(source, fullName, destination string) (string, error) {
	switch {
	case destination == "" && strings.HasPrefix(fullName, "refs/"):
		return fullName, nil
	case destination == "":
		return "", fmt.Errorf("refspec %q names no destination ref", source)
	case strings.HasPrefix(destination, "refs/"):
		return destination, nil
	case strings.HasPrefix(fullName, "refs/tags/"):
		return "refs/tags/" + destination, nil
	}
	return "refs/heads/" + destination, nil
}

// trackingObject is the clone's last-fetched object for a remote branch, or
// the zero object when the clone tracks none.
func trackingObject(repository, remote, destination string) string {
	branch, isBranch := strings.CutPrefix(destination, "refs/heads/")
	if !isBranch {
		return zeroObject
	}
	object, err := gitLine(repository, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch)
	if err != nil {
		return zeroObject
	}
	return object
}

func gitLine(repository string, args ...string) (string, error) {
	output, err := gitOutput(repository, args...)
	return strings.TrimSpace(string(output)), err
}
