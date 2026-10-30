package subject

// Git hook facts: what a pre-push hook is told about each pushed ref, the
// range that ref publishes, and where git runs hooks for a clone.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// PushedRefBase is the base of the range one pushed ref publishes: the pushed
// object compared with the remote's object and the remote's HEAD, else
// origin/HEAD, as publishedBase compares them.
func PushedRefBase(repository string, ref PushedRef) (string, error) {
	base, err := publishedBase(repository, ref.LocalObject, knownCommit(repository, ref.RemoteObject), "refs/remotes/"+ref.Remote+"/HEAD", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref.LocalRef, ErrNoPushedRefBase)
	}
	return base, nil
}

// publishedBase is the base of the range that pushing head over tip publishes.
// Tip is the remote ref's commit, empty for a new ref or one this clone lacks;
// trunks name the remote's default branch, most specific first. A push that
// fast-forwards tip publishes what follows it. Otherwise the base is the best
// common ancestor of head and a merge of tip and the first trunk this clone
// has, so a forced push counts neither the commits it drops nor those a rebase
// brought in from a newer trunk, while the commits it rewrites still count.
// When tip and the trunk leave several best common ancestors, any one of them
// could hide a change to what tip holds, so the base is head's merge base
// with tip alone.
func publishedBase(repository, head, tip string, trunks ...string) (string, error) {
	var published []string
	if tip != "" {
		if _, err := gitOutput(repository, "merge-base", "--is-ancestor", tip, head); err == nil {
			return tip, nil
		}
		published = append(published, tip)
	}
	for _, trunk := range trunks {
		if object := knownCommit(repository, trunk); object != "" {
			published = append(published, object)
			break
		}
	}
	if len(published) == 0 {
		return "", errors.New("nothing published to compare with")
	}
	output, err := gitLine(repository, append([]string{"merge-base", "--all", head}, published...)...)
	if err != nil {
		return "", err
	}
	bases := strings.Fields(output)
	if len(bases) > 1 && len(published) > 1 {
		return gitLine(repository, "merge-base", head, tip)
	}
	return bases[0], nil
}

// knownCommit is the commit a revision names in the clone, empty when it
// names none.
func knownCommit(repository, revision string) string {
	if revision == "" {
		return ""
	}
	object, err := committedRangeResolver{repository: repositoryRoot(repository)}.resolveCommit(revisionName(revision))
	if err != nil {
		return ""
	}
	return string(object)
}

// HookLocations says where git runs hooks for a clone. Directory is absolute
// and shared by linked worktrees; HooksPath is core.hooksPath as configured,
// with a leading ~ expanded, empty when unset. Common is the absolute git
// directory linked worktrees share, whose hooks directory git runs while
// core.hooksPath is unset. SharedHooks lists, resolved for this clone, every
// core.hooksPath set in configuration other than this clone's own config or
// config.worktree file, such as the global file or a file an include pulls in,
// whether or not it wins here: other repositories may run hooks from each. Git
// resolves a relative core.hooksPath from each repository's top directory, so
// only one that stays inside it, or an empty one, serves no other repository.
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
	clone := cloneConfiguration{repository: repository, files: paths[2:]}
	for _, value := range values {
		locations.HooksPath = value.path
		if clone.shares(value) {
			locations.SharedHooks = append(locations.SharedHooks, absoluteIn(repository, value.path))
		}
	}
	return locations, nil
}

type configuredPath struct {
	origin string
	path   string
}

// cloneConfiguration is a clone's top directory and the configuration files
// only it reads: its config and config.worktree.
type cloneConfiguration struct {
	repository string
	files      []string
}

// shares reports whether other repositories may run hooks from a configured
// value: it leaves each repository's top directory and is not set in one of
// the clone's own files.
func (clone cloneConfiguration) shares(value configuredPath) bool {
	if value.path == "" || filepath.IsLocal(value.path) {
		return false
	}
	file, found := strings.CutPrefix(value.origin, "file:")
	if !found {
		return true
	}
	for _, config := range clone.files {
		if sameFile(absoluteIn(clone.repository, file), absoluteIn(clone.repository, config)) {
			return false
		}
	}
	return true
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
