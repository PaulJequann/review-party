package reviewparty

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func readLocalRegularFile(anchor, path, description string, maximumBytes int64) ([]byte, bool, error) {
	return readLocalRegularFileWith(anchor, path, description, maximumBytes, func(root *os.Root, name string) (*os.File, error) {
		return root.Open(name)
	})
}

func readLocalRegularFileWith(anchor, path, description string, maximumBytes int64, open func(*os.Root, string) (*os.File, error)) ([]byte, bool, error) {
	relative, err := filepath.Rel(anchor, path)
	if err != nil {
		return nil, false, fmt.Errorf("resolve %s path %q: %w", description, path, err)
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, false, fmt.Errorf("open %s root %q: %w", description, anchor, err)
	}
	defer root.Close()
	before, err := inspectRootedRegularFile(root, relative, description, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	file, err := open(root, relative)
	if err != nil {
		return nil, false, fmt.Errorf("open %s at %q: %w", description, path, err)
	}
	defer file.Close()
	verification := openedFileVerification{root: root, relative: relative, description: description, path: path, before: before, file: file}
	if err := verifyOpenedLocalFile(verification); err != nil {
		return nil, false, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %q: %w", description, path, err)
	}
	return payload, true, nil
}

func verifyOpenedLocalFile(verification openedFileVerification) error {
	opened, err := verification.file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened %s at %q: %w", verification.description, verification.path, err)
	}
	after, err := inspectRootedRegularFile(verification.root, verification.relative, verification.description, verification.path)
	if err != nil {
		return err
	}
	if !sameOpenedFile(verification.before, opened, after) {
		return fmt.Errorf("%s at %q changed while it was being opened", verification.description, verification.path)
	}
	return nil
}

func sameOpenedFile(before, opened, after fs.FileInfo) bool {
	return opened.Mode().IsRegular() && os.SameFile(before, opened) && os.SameFile(opened, after)
}

func inspectRootedRegularFile(root *os.Root, relative, description, path string) (fs.FileInfo, error) {
	current := ""
	components := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	context := rootedPathContext{description: description, path: path}
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if err := validateRootedPathComponent(info, index == len(components)-1, current, context); err != nil {
			return nil, err
		}
	}
	return root.Lstat(relative)
}

type rootedPathContext struct {
	description string
	path        string
}

type openedFileVerification struct {
	root        *os.Root
	relative    string
	description string
	path        string
	before      fs.FileInfo
	file        *os.File
}

func validateRootedPathComponent(info fs.FileInfo, leaf bool, component string, context rootedPathContext) error {
	if leaf {
		if info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		return fmt.Errorf("%s at %q must be a regular file, not a symlink or special file", context.description, context.path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s at %q must not traverse symlink %q", context.description, context.path, component)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s at %q must traverse directories", context.description, context.path)
	}
	return nil
}
