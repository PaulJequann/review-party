package subject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
)

// viewAttributes stops clean filters, ident expansion, and encoding
// conversion from running while a view is checked out.
const viewAttributes = "* -filter -ident -working-tree-encoding\n"

// InPlace is the repository itself for a Subject that needs no view. Every
// other Subject is a view source: ViewKey names the content and BuildView
// materializes it into an empty directory owned by the runtime.
func (subject Subject) InPlace() (string, bool) {
	if subject.Kind == model.SubjectCapturedChange || subject.HeadObject != "" {
		return "", false
	}
	return subject.Repository, true
}

func (subject Subject) ViewKey() string {
	if subject.Kind == model.SubjectCapturedChange {
		return "captured:" + subject.Identity
	}
	return "git:" + subject.Repository + ":" + subject.HeadObject
}

// BuildView copies a captured change, or checks out the recorded head of a
// Subject with one (a committed range or the unreviewed delta of one) into a
// git view that shares the repository's objects and writes nothing back.
func (subject Subject) BuildView(ctx context.Context, dir string) error {
	switch {
	case subject.Kind == model.SubjectCapturedChange:
		if subject.capturedHead == "" {
			return errors.New("captured Subject execution source is unavailable")
		}
		return subject.copyCaptured(dir)
	case subject.HeadObject != "":
		return gitView{repository: subject.Repository, head: subject.HeadObject, dir: dir}.build(ctx)
	default:
		return fmt.Errorf("%s Subjects are reviewed in place and have no view", subject.Kind)
	}
}

// gitView is a detached checkout of head in dir whose objects are borrowed
// from repository through an alternates file.
type gitView struct {
	repository string
	head       string
	dir        string
}

func (view gitView) build(ctx context.Context) error {
	objects, err := view.query(ctx, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return err
	}
	format, err := view.query(ctx, "rev-parse", "--show-object-format")
	if err != nil {
		return err
	}
	if _, err := runGit(ctx, "", "init", "-q", "--template=", "--object-format="+format, view.dir); err != nil {
		return err
	}
	if err := view.write(filepath.Join(".git", "objects", "info", "alternates"), []byte(objects+"\n")); err != nil {
		return err
	}
	if err := view.write(filepath.Join(".git", "info", "attributes"), []byte(viewAttributes)); err != nil {
		return err
	}
	_, err = runGit(ctx, view.dir, "-c", "core.hooksPath="+os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.longpaths=true", "checkout", "-q", "--force", "--detach", view.head)
	return err
}

func (view gitView) query(ctx context.Context, args ...string) (string, error) {
	output, err := runGit(ctx, view.repository, args...)
	return strings.TrimSpace(string(output)), err
}

func (view gitView) write(relative string, content []byte) error {
	path := filepath.Join(view.dir, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare Subject view: %w", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("prepare Subject view: %w", err)
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_LFS_SKIP_SMUDGE=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.Output()
	if err != nil {
		return nil, gitCommandError(args, err)
	}
	return output, nil
}

func (subject Subject) copyCaptured(destination string) error {
	source := subject.capturedHead
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
