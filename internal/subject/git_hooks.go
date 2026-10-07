package subject

// Git hook facts: what a pre-push hook is told about each pushed ref, the
// range that ref publishes, and where git runs hooks for a clone.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// PushedRef is one line git gives a pre-push hook, with the remote the hook
// was called for.
type PushedRef struct {
	Remote       string
	LocalRef     string
	LocalObject  string
	RemoteRef    string
	RemoteObject string
}

// Deletes reports whether the push deletes the remote ref.
func (ref PushedRef) Deletes() bool { return isZeroObject(ref.LocalObject) }

func isZeroObject(object string) bool { return strings.Trim(object, "0") == "" }

// ParsePushedRefs reads a pre-push hook's standard input: one
// "<local ref> <local object> <remote ref> <remote object>" line per ref.
func ParsePushedRefs(remote string, input []byte) ([]PushedRef, error) {
	var refs []PushedRef
	for _, line := range strings.Split(string(input), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected pre-push input line %q", line)
		}
		refs = append(refs, PushedRef{Remote: remote, LocalRef: fields[0], LocalObject: fields[1], RemoteRef: fields[2], RemoteObject: fields[3]})
	}
	return refs, nil
}

var ErrNoPushedRefBase = errors.New("no remote object, remote HEAD, or origin/HEAD to compare with")

// PushedRefBase is the base of the range one pushed ref publishes. When this
// clone has the remote's object, the base is its merge base with the pushed
// object, so a forced push does not count the commits it drops. A new ref, or
// a remote object this clone lacks, uses the merge base with the remote's
// HEAD, else with origin/HEAD.
func PushedRefBase(repository string, ref PushedRef) (string, error) {
	candidates := []string{"refs/remotes/" + ref.Remote + "/HEAD", "refs/remotes/origin/HEAD"}
	if _, err := gitOutput(repository, "cat-file", "-e", ref.RemoteObject+"^{commit}"); err == nil {
		candidates = []string{ref.RemoteObject}
	}
	for _, candidate := range candidates {
		if output, err := gitOutput(repository, "merge-base", ref.LocalObject, candidate); err == nil {
			return strings.TrimSpace(string(output)), nil
		}
	}
	return "", fmt.Errorf("%s: %w", ref.LocalRef, ErrNoPushedRefBase)
}

// HookLocations says where git runs hooks for a clone. Directory is absolute
// and shared by linked worktrees; HooksPath is core.hooksPath as configured,
// empty when unset. Common is the absolute git directory linked worktrees
// share, whose hooks directory git runs while core.hooksPath is unset.
type HookLocations struct {
	Directory string
	HooksPath string
	Common    string
}

func ResolveHookLocations(repository string) (HookLocations, error) {
	directories, err := gitOutput(repository, "rev-parse", "--git-path", "hooks", "--git-common-dir")
	if err != nil {
		return HookLocations{}, fmt.Errorf("find the hooks directory: %w", err)
	}
	hooksPath, err := gitOutput(repository, "config", "--default", "", "--get", "core.hooksPath")
	if err != nil {
		return HookLocations{}, fmt.Errorf("read core.hooksPath: %w", err)
	}
	directory, common, _ := strings.Cut(strings.TrimSuffix(string(directories), "\n"), "\n")
	return HookLocations{
		Directory: absoluteIn(repository, directory),
		HooksPath: strings.TrimSuffix(string(hooksPath), "\n"),
		Common:    absoluteIn(repository, common),
	}, nil
}

func absoluteIn(repository, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repository, path)
}
