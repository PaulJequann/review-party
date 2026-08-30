package subject

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
	"syscall"
	"unicode"
)

const executionRootName = "review-party-worktrees"
const maxReconciledCheckouts = 100

// ExecutionCheckout hides the complete lifecycle of the repository view used
// by one Review attempt. Close must run only after the Reviewer process exits.
type ExecutionCheckout struct {
	Repository string
	close      func() error
}

func (checkout *ExecutionCheckout) Close() error {
	if checkout == nil || checkout.close == nil {
		return nil
	}
	err := checkout.close()
	checkout.close = nil
	return err
}

type checkoutOwner struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	PID        int    `json:"pid"`
}

type ownedCheckout struct {
	root     executionRoot
	metadata metadataPath
	owner    checkoutOwner
}

type executionRoot string
type metadataPath string
type ownerLabel string
type repositoryPath string
type checkoutPath string

func (subject Subject) PrepareExecution(owner string) (*ExecutionCheckout, error) {
	switch subject.Kind {
	case model.SubjectCapturedChange:
		return subject.prepareCapturedExecution(owner)
	case model.SubjectCommittedRange:
		return prepareCommittedExecution(subject.ReviewSubject, owner)
	case model.SubjectWorkingChanges:
		return &ExecutionCheckout{Repository: subject.Repository}, nil
	default:
		return &ExecutionCheckout{Repository: subject.Repository}, nil
	}
}

func prepareCommittedExecution(subject model.ReviewSubject, owner string) (*ExecutionCheckout, error) {
	root := executionRoot(filepath.Join(os.TempDir(), executionRootName))
	if err := os.MkdirAll(string(root), 0o700); err != nil {
		return nil, fmt.Errorf("create Subject execution root: %w", err)
	}
	if err := reconcileOwnedCheckouts(root); err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(string(root), safeOwner(ownerLabel(owner))+"-")
	if err != nil {
		return nil, fmt.Errorf("allocate Subject execution checkout: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("prepare Subject execution path: %w", err)
	}
	metadata := metadataPath(path + ".owner.json")
	owned := checkoutOwner{Repository: subject.Repository, Path: path, PID: os.Getpid()}
	if err := writeOwner(metadata, owned); err != nil {
		return nil, err
	}
	if _, err := gitOutput(subject.Repository, "worktree", "add", "--detach", path, subject.HeadObject); err != nil {
		return nil, errors.Join(fmt.Errorf("create Subject execution checkout: %w", err), os.Remove(string(metadata)))
	}
	checkout := ownedCheckout{root: root, metadata: metadata, owner: owned}
	return &ExecutionCheckout{Repository: path, close: func() error { return removeOwnedCheckout(checkout) }}, nil
}

func (subject Subject) prepareCapturedExecution(owner string) (*ExecutionCheckout, error) {
	if subject.capturedHead == "" {
		return nil, errors.New("captured Subject execution source is unavailable")
	}
	root := filepath.Join(os.TempDir(), executionRootName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	destination, err := os.MkdirTemp(root, safeOwner(ownerLabel(owner))+"-captured-")
	if err != nil {
		return nil, err
	}
	if err := copyCapturedTree(subject.capturedHead, destination); err != nil {
		return nil, errors.Join(err, os.RemoveAll(destination))
	}
	return &ExecutionCheckout{Repository: destination, close: func() error { return os.RemoveAll(destination) }}, nil
}

func copyCapturedTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.Name() == ".git" {
			return fmt.Errorf("captured Subject contains forbidden entry %q", path)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, payload, 0o600)
	})
}

func safeOwner(value ownerLabel) string {
	cleaned := strings.Map(func(r rune) rune {
		if validOwnerRune(r) {
			return r
		}
		return '-'
	}, string(value))
	if cleaned == "" {
		return "attempt"
	}
	return cleaned
}

func validOwnerRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || strings.ContainsRune("-_", value)
}

func writeOwner(path metadataPath, owner checkoutOwner) error {
	payload, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	if err := os.WriteFile(string(path), payload, 0o600); err != nil {
		return fmt.Errorf("write Subject execution ownership: %w", err)
	}
	return nil
}

func reconcileOwnedCheckouts(root executionRoot) error {
	entries, err := filepath.Glob(filepath.Join(string(root), "*.owner.json"))
	if err != nil {
		return err
	}
	if len(entries) > maxReconciledCheckouts {
		entries = entries[:maxReconciledCheckouts]
	}
	for _, metadata := range entries {
		payload, err := os.ReadFile(metadata)
		if err != nil {
			return err
		}
		var owner checkoutOwner
		if err := json.Unmarshal(payload, &owner); err != nil {
			continue
		}
		if processAlive(owner.PID) {
			continue
		}
		if err := removeOwnedCheckout(ownedCheckout{root: root, metadata: metadataPath(metadata), owner: owner}); err != nil {
			return fmt.Errorf("reconcile Subject execution checkout: %w", err)
		}
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func removeOwnedCheckout(checkout ownedCheckout) error {
	path, err := validateOwnedPath(checkout)
	if err != nil {
		return err
	}
	registered, err := registeredWorktree(repositoryPath(checkout.owner.Repository), path)
	if err != nil {
		return err
	}
	if registered {
		if _, err := gitOutput(checkout.owner.Repository, "worktree", "remove", "--force", string(path)); err != nil {
			return fmt.Errorf("remove Subject execution checkout: %w", err)
		}
	}
	if err := os.Remove(string(checkout.metadata)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validateOwnedPath(checkout ownedCheckout) (checkoutPath, error) {
	root, err := filepath.Abs(string(checkout.root))
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(checkout.owner.Path)
	if err != nil {
		return "", err
	}
	if filepath.Dir(path) != root || string(checkout.metadata) != path+".owner.json" {
		return "", fmt.Errorf("refuse unowned Subject execution path %q", path)
	}
	return checkoutPath(path), nil
}

func registeredWorktree(repository repositoryPath, target checkoutPath) (bool, error) {
	value, err := gitOutput(string(repository), "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, err
	}
	for _, field := range strings.Split(string(value), "\x00") {
		if strings.HasPrefix(field, "worktree ") && strings.TrimPrefix(field, "worktree ") == string(target) {
			return true, nil
		}
	}
	return false, nil
}
