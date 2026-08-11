package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reviewparty/internal/model"
)

type RecordStore interface {
	Save(model.ReviewRecord) error
	Load(model.ReviewID) (model.ReviewRecord, error)
}

type FileRecordStore struct {
	directory string
}

func NewFileRecordStore(directory string) (*FileRecordStore, error) {
	if directory == "" {
		return nil, errors.New("record directory is required")
	}
	return &FileRecordStore{directory: directory}, nil
}

func (store *FileRecordStore) Save(record model.ReviewRecord) error {
	payload, err := encodeReviewRecord(record)
	if err != nil {
		return err
	}
	temporary, err := store.createTemporaryRecord()
	if err != nil {
		return err
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

func encodeReviewRecord(record model.ReviewRecord) ([]byte, error) {
	if record.SchemaVersion != model.CurrentReviewRecordSchemaVersion {
		return nil, fmt.Errorf("save review record schema %d: current schema is %d", record.SchemaVersion, model.CurrentReviewRecordSchemaVersion)
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode review record: %w", err)
	}
	return payload, nil
}

func (store *FileRecordStore) createTemporaryRecord() (*os.File, error) {
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return nil, fmt.Errorf("create record directory: %w", err)
	}
	temporary, err := os.CreateTemp(store.directory, ".record-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary review record: %w", err)
	}
	return temporary, nil
}

func (store *FileRecordStore) Load(id model.ReviewID) (model.ReviewRecord, error) {
	payload, err := os.ReadFile(store.path(id))
	if err != nil {
		return model.ReviewRecord{}, fmt.Errorf("read review record %q: %w", id, err)
	}
	var record model.ReviewRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return model.ReviewRecord{}, fmt.Errorf("decode review record %q: %w", id, err)
	}
	if err := normalizeLoadedReviewRecord(&record); err != nil {
		return model.ReviewRecord{}, fmt.Errorf("decode review record %q: %w", id, err)
	}
	if record.ID != id {
		return model.ReviewRecord{}, fmt.Errorf("review record %q has mismatched identity %q", id, record.ID)
	}
	return record, nil
}

func normalizeLoadedReviewRecord(record *model.ReviewRecord) error {
	if record.SchemaVersion == 0 {
		record.SchemaVersion = model.LegacyReviewRecordSchemaVersion
	}
	if record.SchemaVersion > model.CurrentReviewRecordSchemaVersion {
		return fmt.Errorf("record schema %d is newer than supported schema %d", record.SchemaVersion, model.CurrentReviewRecordSchemaVersion)
	}
	if record.SchemaVersion < model.LegacyReviewRecordSchemaVersion {
		return fmt.Errorf("record schema %d is invalid", record.SchemaVersion)
	}
	return nil
}

func (store *FileRecordStore) path(id model.ReviewID) string {
	return filepath.Join(store.directory, string(id)+".json")
}
