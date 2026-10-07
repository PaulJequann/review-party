package subject

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"reviewparty/internal/model"
	"sort"
	"strings"
)

const emptyGitTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Subject owns the correspondence between a durable ReviewSubject and the
// material needed to prepare one Reviewer execution. Patch feeds the identity
// hash and the prompt but is never persisted. The repository can regenerate it.
type Subject struct {
	model.ReviewSubject
	Patch          string
	capturedHead   string
	capturedDigest string
}

func ResolveSubject(repository string, reference model.SubjectReference) (Subject, error) {
	root := repositoryRoot(repository)
	switch reference.Kind {
	case model.SubjectWorkingChanges:
		return resolveWorkingChangesAtRoot(root)
	case model.SubjectCommittedRange:
		return resolveCommittedRange(root, reference)
	case model.SubjectCapturedChange:
		return resolveCapturedChange(reference)
	case model.SubjectUnreviewedDelta:
		return Subject{}, fmt.Errorf("%s subjects are measured from reviewed state by run --unreviewed, not resolved from a reference", reference.Kind)
	default:
		return Subject{}, fmt.Errorf("unsupported review subject %q", reference.Kind)
	}
}

func resolveCapturedChange(reference model.SubjectReference) (Subject, error) {
	baseValue, err := filepath.Abs(reference.CapturedBase)
	if err != nil {
		return Subject{}, err
	}
	headValue, err := filepath.Abs(reference.CapturedHead)
	if err != nil {
		return Subject{}, err
	}
	base := capturedDirectory(baseValue)
	head := capturedDirectory(headValue)
	if _, err := digestCapturedDirectory(base); err != nil {
		return Subject{}, fmt.Errorf("validate captured base: %w", err)
	}
	digest, err := digestCapturedDirectory(head)
	if err != nil {
		return Subject{}, fmt.Errorf("validate captured head: %w", err)
	}
	patch, err := capturedDirectoryPatch(base, head)
	if err != nil {
		return Subject{}, err
	}
	if len(patch) == 0 {
		return Subject{}, errors.New("captured change is empty")
	}
	paths, err := changedCapturedPaths(base, head)
	if err != nil {
		return Subject{}, err
	}
	identity := sha256.Sum256(append([]byte(strings.Join(paths, "\x00")+"\x00"), patch...))
	facts := model.SubjectFacts{ChangedFiles: len(paths)}
	return Subject{ReviewSubject: model.ReviewSubject{Kind: model.SubjectCapturedChange, Repository: "eval://" + hex.EncodeToString(identity[:]), Identity: hex.EncodeToString(identity[:]), ChangedPaths: paths, Facts: &facts}, Patch: string(patch), capturedHead: string(head), capturedDigest: digest}, nil
}

type capturedDirectory string
type capturedFile string

// digestCapturedDirectory rejects symlinks and Git metadata and hashes every
// file, not just the changed ones, so two captures sharing a patch never share
// a view.
func digestCapturedDirectory(directory capturedDirectory) (string, error) {
	info, err := os.Stat(string(directory))
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", string(directory))
	}
	tree := capturedTree{root: string(directory), hash: sha256.New()}
	err = filepath.WalkDir(tree.root, tree.visit)
	return hex.EncodeToString(tree.hash.Sum(nil)), err
}

type capturedTree struct {
	root string
	hash hash.Hash
}

func (tree capturedTree) visit(path string, entry os.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("captured Subject contains symlink %q", path)
	}
	if entry.Name() == ".git" {
		return fmt.Errorf("captured Subject contains forbidden Git metadata %q", path)
	}
	relative, err := filepath.Rel(tree.root, path)
	if err != nil {
		return err
	}
	if entry.IsDir() {
		tree.hash.Write(fmt.Appendf(nil, "d %s\x00", filepath.ToSlash(relative)))
		return nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tree.hash.Write(fmt.Appendf(nil, "f %s\x00%d\x00", filepath.ToSlash(relative), len(payload)))
	tree.hash.Write(payload)
	return nil
}

func capturedDirectoryPatch(base, head capturedDirectory) ([]byte, error) {
	command := exec.Command("git", "diff", "--no-index", "--binary", "--", string(base), string(head))
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			return nil, fmt.Errorf("capture synthetic change: %w", err)
		}
	}
	patch := string(output)
	patch = strings.ReplaceAll(patch, "a"+string(base)+"/", "a/")
	patch = strings.ReplaceAll(patch, "b"+string(head)+"/", "b/")
	patch = strings.ReplaceAll(patch, string(base)+"/", "a/")
	patch = strings.ReplaceAll(patch, string(head)+"/", "b/")
	return []byte(patch), nil
}

func changedCapturedPaths(base, head capturedDirectory) ([]string, error) {
	files := map[string]string{}
	for _, directory := range []capturedDirectory{base, head} {
		if err := collectCapturedFiles(directory, files); err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func collectCapturedFiles(directory capturedDirectory, files map[string]string) error {
	return filepath.WalkDir(string(directory), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		key, digest, err := capturedFileIdentity(directory, capturedFile(path))
		if err != nil {
			return err
		}
		if files[key] == digest {
			delete(files, key)
		} else {
			files[key] = digest
		}
		return nil
	})
}

func capturedFileIdentity(directory capturedDirectory, path capturedFile) (string, string, error) {
	relative, err := filepath.Rel(string(directory), string(path))
	if err != nil {
		return "", "", err
	}
	payload, err := os.ReadFile(string(path))
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(payload)
	return filepath.ToSlash(relative), hex.EncodeToString(digest[:]), nil
}

type repositoryRoot string

func resolveCommittedRange(root repositoryRoot, reference model.SubjectReference) (Subject, error) {
	if reference.Base == "" || reference.Head == "" {
		return Subject{}, errors.New("committed range requires both base and head revisions")
	}
	resolver := committedRangeResolver{repository: root}
	base, head, err := resolver.resolveRange(revisionName(reference.Base), revisionName(reference.Head))
	if err != nil {
		return Subject{}, err
	}
	patch, err := resolver.patch(base, head)
	if err != nil {
		return Subject{}, err
	}
	if len(patch) == 0 {
		return Subject{}, errors.New("committed range is empty")
	}
	paths, facts, err := measureCapturedPatch(string(root), patch)
	if err != nil {
		return Subject{}, err
	}
	facts.ChangedFiles = len(paths)
	changes, err := root.treeContentChanges(revisionName(base), revisionName(head))
	if err != nil {
		return Subject{}, err
	}
	capture := committedRangeCapture{repository: root, base: base, head: head, patch: patch, paths: paths, facts: facts, changes: changes}
	return capture.subject(), nil
}

type committedRangeResolver struct{ repository repositoryRoot }
type commitObject string
type revisionName string

func (resolver committedRangeResolver) resolveRange(base, head revisionName) (commitObject, commitObject, error) {
	baseObject, err := resolver.resolveCommit(base)
	if err != nil {
		return "", "", fmt.Errorf("resolve base revision: %w", err)
	}
	headObject, err := resolver.resolveCommit(head)
	if err != nil {
		return "", "", fmt.Errorf("resolve head revision: %w", err)
	}
	return baseObject, headObject, nil
}

func (resolver committedRangeResolver) resolveCommit(revision revisionName) (commitObject, error) {
	value, err := gitOutput(string(resolver.repository), "rev-parse", "--verify", string(revision)+"^{commit}")
	if err != nil {
		return "", err
	}
	return commitObject(strings.TrimSpace(string(value))), nil
}

func (resolver committedRangeResolver) patch(base, head commitObject) ([]byte, error) {
	patch, err := gitOutput(string(resolver.repository), "diff", "--binary", "--no-ext-diff", string(base), string(head), "--")
	if err != nil {
		return nil, fmt.Errorf("capture committed range: %w", err)
	}
	return patch, nil
}

type committedRangeCapture struct {
	repository repositoryRoot
	base, head commitObject
	patch      []byte
	paths      []string
	facts      model.SubjectFacts
	changes    []model.ContentChange
}

func (capture committedRangeCapture) subject() Subject {
	hash := sha256.New()
	for _, value := range []string{string(model.SubjectCommittedRange), string(capture.repository), string(capture.base), string(capture.head)} {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	hash.Write(capture.patch)
	return Subject{ReviewSubject: model.ReviewSubject{Kind: model.SubjectCommittedRange, Repository: string(capture.repository), Identity: hex.EncodeToString(hash.Sum(nil)), BaseObject: string(capture.base), HeadObject: string(capture.head), ChangedPaths: capture.paths, Facts: &capture.facts, ContentChanges: capture.changes}, Patch: string(capture.patch)}
}

func ResolveWorkingChanges(repository string) (Subject, error) {
	root, err := ResolveRepositoryRoot(repository)
	if err != nil {
		return Subject{}, err
	}
	return resolveWorkingChangesAtRoot(repositoryRoot(root))
}

func resolveWorkingChangesAtRoot(root repositoryRoot) (Subject, error) {
	capture, err := captureWorkingChanges(string(root))
	if err != nil {
		return Subject{}, err
	}
	return newWorkingChangesSubject(root, capture), nil
}

func newWorkingChangesSubject(root repositoryRoot, capture workingChangesCapture) Subject {
	hash := sha256.New()
	hash.Write([]byte(model.SubjectWorkingChanges))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(capture.paths, "\x00")))
	hash.Write([]byte{0})
	hash.Write(capture.patch)

	return Subject{ReviewSubject: model.ReviewSubject{
		Kind:           model.SubjectWorkingChanges,
		Repository:     string(root),
		Identity:       hex.EncodeToString(hash.Sum(nil)),
		ChangedPaths:   capture.paths,
		Facts:          &capture.facts,
		ContentChanges: capture.changes,
	}, Patch: string(capture.patch)}
}

func ResolveRepositoryRoot(repository string) (string, error) {
	root, err := gitOutput(repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	repositoryRoot, err := filepath.Abs(strings.TrimSpace(string(root)))
	if err != nil {
		return "", fmt.Errorf("make repository root absolute: %w", err)
	}
	return repositoryRoot, nil
}

// Push range sources name where DefaultPushBase found the base of the range
// a push would publish.
const (
	PushBaseUpstream   = "upstream"
	PushBaseRemoteHead = "remote-head"
)

var ErrNoDefaultPushBase = errors.New("no upstream branch and no origin/HEAD to compare with")

// DefaultPushBase is the merge base of HEAD and the current branch's
// upstream, else of HEAD and origin/HEAD. A merge base keeps a diverged
// upstream's own commits out of the range.
func DefaultPushBase(repository string) (base, source string, err error) {
	if output, err := gitOutput(repository, "merge-base", "HEAD", "@{upstream}"); err == nil {
		return strings.TrimSpace(string(output)), PushBaseUpstream, nil
	}
	if output, err := gitOutput(repository, "merge-base", "HEAD", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(string(output)), PushBaseRemoteHead, nil
	}
	return "", "", ErrNoDefaultPushBase
}

// readGitConfig returns the clone's configuration keyed as git config --list
// prints keys, with the section and variable in lowercase and the subsection
// as written. A key set more than once keeps its last value, as git config
// --get does.
func readGitConfig(repository string) (map[string]string, error) {
	output, err := gitOutput(repository, "config", "--list", "-z")
	if err != nil {
		return nil, err
	}
	config := map[string]string{}
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(output), "\x00"), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		config[key] = value
	}
	return config, nil
}

func gitLine(repository string, args ...string) (string, error) {
	output, err := gitOutput(repository, args...)
	return strings.TrimSpace(string(output)), err
}

func gitOutput(repository string, args ...string) ([]byte, error) {
	return gitInputOutput(repository, nil, args...)
}

func gitInputOutput(repository string, input []byte, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = repository
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	output, err := command.Output()
	if err != nil {
		return nil, gitCommandError(args, err)
	}
	return output, nil
}

func gitCommandError(args []string, err error) error {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitError.Stderr)))
	}
	return err
}

func splitNUL(value []byte) []string {
	parts := bytes.Split(value, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}
