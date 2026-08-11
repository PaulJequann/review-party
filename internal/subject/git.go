package subject

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
)

const emptyGitTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func ResolveSubject(repository string, reference model.SubjectReference) (model.ReviewSubject, error) {
	root := repositoryRoot(repository)
	switch reference.Kind {
	case model.SubjectWorkingChanges:
		return resolveWorkingChangesAtRoot(root)
	case model.SubjectCommittedRange:
		return resolveCommittedRange(root, reference)
	default:
		return model.ReviewSubject{}, fmt.Errorf("unsupported review subject %q", reference.Kind)
	}
}

type repositoryRoot string

func resolveCommittedRange(root repositoryRoot, reference model.SubjectReference) (model.ReviewSubject, error) {
	if reference.Base == "" || reference.Head == "" {
		return model.ReviewSubject{}, errors.New("committed range requires both base and head revisions")
	}
	resolver := committedRangeResolver{repository: root}
	base, err := resolver.resolveCommit(revisionName(reference.Base))
	if err != nil {
		return model.ReviewSubject{}, fmt.Errorf("resolve base revision: %w", err)
	}
	head, err := resolver.resolveCommit(revisionName(reference.Head))
	if err != nil {
		return model.ReviewSubject{}, fmt.Errorf("resolve head revision: %w", err)
	}
	patch, err := resolver.patch(base, head)
	if err != nil {
		return model.ReviewSubject{}, err
	}
	if len(patch) == 0 {
		return model.ReviewSubject{}, errors.New("committed range is empty")
	}
	paths, facts, err := measureCapturedPatch(string(root), patch)
	if err != nil {
		return model.ReviewSubject{}, err
	}
	facts.ChangedFiles = len(paths)
	capture := committedRangeCapture{repository: root, base: base, head: head, patch: patch, paths: paths, facts: facts}
	return capture.subject(), nil
}

type committedRangeResolver struct{ repository repositoryRoot }
type commitObject string
type revisionName string

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
}

func (capture committedRangeCapture) subject() model.ReviewSubject {
	hash := sha256.New()
	for _, value := range []string{string(model.SubjectCommittedRange), string(capture.repository), string(capture.base), string(capture.head)} {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	hash.Write(capture.patch)
	return model.ReviewSubject{Kind: model.SubjectCommittedRange, Repository: string(capture.repository), Identity: hex.EncodeToString(hash.Sum(nil)), BaseObject: string(capture.base), HeadObject: string(capture.head), ChangedPaths: capture.paths, Patch: string(capture.patch), Facts: &capture.facts}
}

func ResolveWorkingChanges(repository string) (model.ReviewSubject, error) {
	root, err := ResolveRepositoryRoot(repository)
	if err != nil {
		return model.ReviewSubject{}, err
	}
	return resolveWorkingChangesAtRoot(repositoryRoot(root))
}

func resolveWorkingChangesAtRoot(root repositoryRoot) (model.ReviewSubject, error) {
	capture, err := captureWorkingChanges(string(root))
	if err != nil {
		return model.ReviewSubject{}, err
	}
	return newWorkingChangesSubject(root, capture), nil
}

func newWorkingChangesSubject(root repositoryRoot, capture workingChangesCapture) model.ReviewSubject {
	hash := sha256.New()
	hash.Write([]byte(model.SubjectWorkingChanges))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(capture.paths, "\x00")))
	hash.Write([]byte{0})
	hash.Write(capture.patch)

	return model.ReviewSubject{
		Kind:         model.SubjectWorkingChanges,
		Repository:   string(root),
		Identity:     hex.EncodeToString(hash.Sum(nil)),
		ChangedPaths: capture.paths,
		Patch:        string(capture.patch),
		Facts:        &capture.facts,
	}
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

func gitOutput(repository string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = repository
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitError.Stderr)))
	}
	return nil, err
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
