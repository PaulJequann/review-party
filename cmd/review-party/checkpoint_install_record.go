package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"reviewparty/internal/subject"
)

const installRecordName = "review-party-created.json"

type installRecord struct {
	path    string
	Created []createdFile `json:"created"`
}

type createdFile struct {
	Path        string   `json:"path"`
	Directories []string `json:"directories,omitempty"`
}

func readInstallRecord(root string) (installRecord, error) {
	locations, err := subject.ResolveHookLocations(root)
	if err != nil {
		return installRecord{}, err
	}
	record := installRecord{path: filepath.Join(locations.Common, installRecordName)}
	content, err := os.ReadFile(record.path)
	if errors.Is(err, fs.ErrNotExist) {
		return record, nil
	}
	if err == nil {
		err = json.Unmarshal(content, &record)
	}
	if err != nil {
		return installRecord{}, fmt.Errorf("read %s: %w", record.path, err)
	}
	return record, nil
}

// recordCreations runs before the writes, so a failed write leaves a record
// of a missing file, which uninstall forgets, rather than an unrecorded file.
func recordCreations(root string, paths []string) error {
	var created []createdFile
	for _, path := range paths {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			created = append(created, createdFile{Path: path, Directories: missingDirectories(filepath.Dir(path))})
		}
	}
	if len(created) == 0 {
		return nil
	}
	record, err := readInstallRecord(root)
	if err != nil {
		return err
	}
	for _, file := range created {
		record.forget(file.Path)
		record.Created = append(record.Created, file)
	}
	return record.save()
}

func missingDirectories(directory string) []string {
	var missing []string
	for {
		if _, err := os.Lstat(directory); !errors.Is(err, fs.ErrNotExist) {
			return missing
		}
		missing = append(missing, directory)
		parent := filepath.Dir(directory)
		if parent == directory {
			return missing
		}
		directory = parent
	}
}

func removeCreated(files []createdFile) error {
	var errs []error
	var directories []string
	for _, file := range files {
		errs = append(errs, removeIfPresent(file.Path))
		directories = append(directories, file.Directories...)
	}
	slices.SortFunc(directories, func(left, right string) int { return cmp.Compare(len(right), len(left)) })
	for _, directory := range directories {
		errs = append(errs, removeEmptyDirectory(directory))
	}
	return errors.Join(errs...)
}

func removeEmptyDirectory(directory string) error {
	if entries, err := os.ReadDir(directory); err != nil || len(entries) > 0 {
		return nil
	}
	return os.Remove(directory)
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (record installRecord) created(path string) (createdFile, bool) {
	index := slices.IndexFunc(record.Created, func(file createdFile) bool { return file.Path == path })
	if index < 0 {
		return createdFile{}, false
	}
	return record.Created[index], true
}

func (record *installRecord) forget(path string) {
	record.Created = slices.DeleteFunc(record.Created, func(file createdFile) bool { return file.Path == path })
}

func (record installRecord) save() error {
	if len(record.Created) > 0 {
		content, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return err
		}
		return replaceFile(record.path, append(content, '\n'), 0o644)
	}
	return removeIfPresent(record.path)
}
