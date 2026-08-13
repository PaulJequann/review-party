package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func copyFixtureDirectory(source fs.FS, sourceRoot, destination string, packaged bool) ([][]byte, error) {
	paths, err := fixtureFilePaths(source, sourceRoot)
	if err != nil {
		return nil, err
	}
	payloads := make([][]byte, 0, len(paths))
	for _, path := range paths {
		payload, err := copyFixtureFile(fixtureFileCopy{source: source, sourceRoot: sourceRoot, destination: destination, path: path, packaged: packaged})
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

type fixtureFileCopy struct {
	source      fs.FS
	sourceRoot  string
	destination string
	path        string
	packaged    bool
}

func fixtureFilePaths(source fs.FS, sourceRoot string) ([]string, error) {
	var paths []string
	err := fs.WalkDir(source, sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sourceRoot {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || entry.Name() == ".git" {
			return fmt.Errorf("eval fixture contains forbidden entry %q", path)
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func copyFixtureFile(request fixtureFileCopy) ([]byte, error) {
	payload, err := fs.ReadFile(request.source, request.path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(request.sourceRoot, request.path)
	if err != nil {
		return nil, err
	}
	if request.packaged {
		relative = materializedPackagedFixturePath(relative)
	}
	target := filepath.Join(request.destination, relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, payload, 0o600); err != nil {
		return nil, err
	}
	return append([]byte(filepath.ToSlash(relative)+"\x00"), payload...), nil
}

func materializedPackagedFixturePath(relative string) string {
	if filepath.Base(relative) == "go.mod.txt" {
		return filepath.Join(filepath.Dir(relative), "go.mod")
	}
	return relative
}

func decodeStrict(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
