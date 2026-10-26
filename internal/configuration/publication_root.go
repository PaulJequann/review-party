package configuration

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func openPublicationRoot(scope Scope, anchor string) (_ *os.Root, returnErr error) {
	root, err := os.OpenRoot(anchor)
	if err == nil || scope != ScopeGlobal {
		return root, err
	}

	var missing []string
	candidate := filepath.Clean(anchor)
	for err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if _, inspectErr := os.Lstat(candidate); !errors.Is(inspectErr, fs.ErrNotExist) {
			return nil, errors.Join(fmt.Errorf("cannot create publication ancestor %q: %w", candidate, err), inspectErr)
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = filepath.Dir(candidate)
		root, err = os.OpenRoot(candidate)
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, root.Close())
		}
	}()

	// Keep each opened directory as the bound for the next component. Reopening
	// the absolute anchor would lose containment if an ancestor were replaced.
	for index := len(missing) - 1; index >= 0; index-- {
		next, err := openPublicationChild(root, missing[index])
		if err != nil {
			return nil, err
		}
		err = root.Close()
		root = next
		if err != nil {
			return nil, err
		}
	}
	return root, nil
}

func openPublicationChild(parent *os.Root, name string) (_ *os.Root, returnErr error) {
	if err := parent.Mkdir(name, globalDirectoryPermissions); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("create publication ancestor %q: %w", name, err)
	}
	request := rootedPathRequest{relative: name, path: name, description: "publication ancestor", directory: true}
	before, err := inspectRootedPath(parent, request)
	if err != nil {
		return nil, err
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, child.Close())
		}
	}()
	opened, err := child.Stat(".")
	if err != nil {
		return nil, err
	}
	after, err := inspectRootedPath(parent, request)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return nil, fmt.Errorf("publication ancestor %q changed while it was being opened", name)
	}
	return child, nil
}
