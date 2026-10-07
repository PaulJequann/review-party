package subject

// Git hook facts: what a pre-push hook is told about each pushed ref, the
// range that ref publishes, and where git runs hooks for a clone.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// with a leading ~ expanded, empty when unset. Common is the absolute git
// directory linked worktrees share, whose hooks directory git runs while
// core.hooksPath is unset. SharedHooks lists every absolute core.hooksPath
// set in configuration other than this clone's own config or config.worktree
// file, such as the global file or a file an include pulls in, whether or not
// it wins here: other repositories may run hooks from each. A relative
// core.hooksPath resolves inside each clone, so it is never shared.
type HookLocations struct {
	Directory   string
	HooksPath   string
	SharedHooks []string
	Common      string
}

func ResolveHookLocations(repository string) (HookLocations, error) {
	directories, err := gitOutput(repository, "rev-parse", "--git-path", "hooks", "--git-common-dir", "--git-path", "config", "--git-path", "config.worktree")
	if err != nil {
		return HookLocations{}, fmt.Errorf("find the hooks directory: %w", err)
	}
	paths := strings.Split(strings.TrimSuffix(string(directories), "\n"), "\n")
	if len(paths) != 4 {
		return HookLocations{}, fmt.Errorf("find the hooks directory: unexpected git output %q", directories)
	}
	values, err := configuredHooksPaths(repository)
	if err != nil {
		return HookLocations{}, err
	}
	locations := HookLocations{Directory: absoluteIn(repository, paths[0]), Common: absoluteIn(repository, paths[1])}
	for _, value := range values {
		locations.HooksPath = value.path
		own := slices.ContainsFunc(paths[2:], func(config string) bool {
			file, found := strings.CutPrefix(value.origin, "file:")
			return found && sameFile(absoluteIn(repository, file), absoluteIn(repository, config))
		})
		if filepath.IsAbs(value.path) && !own {
			locations.SharedHooks = append(locations.SharedHooks, value.path)
		}
	}
	return locations, nil
}

type configuredPath struct {
	origin string
	path   string
}

// configuredHooksPaths lists every core.hooksPath git reads for the clone,
// with its origin, in the order git reads them, so the last one wins.
func configuredHooksPaths(repository string) ([]configuredPath, error) {
	command := exec.Command("git", "config", "-z", "--show-origin", "--type=path", "--get-all", "core.hooksPath")
	command.Dir = repository
	output, err := command.Output()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read core.hooksPath: %w", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("read core.hooksPath: unexpected git output %q", output)
	}
	var values []configuredPath
	for index := 0; index < len(fields); index += 2 {
		values = append(values, configuredPath{origin: fields[index], path: fields[index+1]})
	}
	return values, nil
}

func sameFile(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func absoluteIn(repository, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repository, path)
}
