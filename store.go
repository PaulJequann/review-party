package reviewparty

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type recordStore interface {
	Save(ReviewRecord) error
	Load(ReviewID) (ReviewRecord, error)
}

type fileRecordStore struct {
	directory string
}

func newFileRecordStore(directory string) (*fileRecordStore, error) {
	if directory == "" {
		return nil, errors.New("record directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create record directory: %w", err)
	}
	return &fileRecordStore{directory: directory}, nil
}

func (store *fileRecordStore) Save(record ReviewRecord) error {
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode review record: %w", err)
	}
	temporary, err := os.CreateTemp(store.directory, ".record-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary review record: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("restrict temporary review record: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary review record: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary review record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary review record: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path(record.ID)); err != nil {
		return fmt.Errorf("publish review record: %w", err)
	}
	return nil
}

func (store *fileRecordStore) Load(id ReviewID) (ReviewRecord, error) {
	payload, err := os.ReadFile(store.path(id))
	if err != nil {
		return ReviewRecord{}, fmt.Errorf("read review record %q: %w", id, err)
	}
	var record ReviewRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return ReviewRecord{}, fmt.Errorf("decode review record %q: %w", id, err)
	}
	if record.ID != id {
		return ReviewRecord{}, fmt.Errorf("review record %q has mismatched identity %q", id, record.ID)
	}
	return record, nil
}

func (store *fileRecordStore) path(id ReviewID) string {
	return filepath.Join(store.directory, string(id)+".json")
}
