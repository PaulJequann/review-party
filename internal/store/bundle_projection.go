package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

// bundleProjection owns the relational mapping for Review Bundle aggregates.
type bundleProjection struct{ db *sql.DB }

type bundlePayload struct{ members, termination, selection, warnings, deduplicated []byte }
type bundleMutablePayload struct{ members, termination []byte }

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// create writes the bundle row and every member's pending Review Record in one
// transaction, so a caller never observes a bundle whose members are missing.
func (p bundleProjection) create(bundle model.ReviewBundle, members []model.ReviewRecord) (returnErr error) {
	payloads, err := renderBundlePayload(bundle)
	if err != nil {
		return err
	}
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin Review Bundle write: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	if _, err := tx.Exec(`INSERT INTO review_bundles(id,description,revision,repository,subject_kind,subject_identity,lifecycle,termination,selection,warnings,deduplicated,members,concurrency_limit,created_at,updated_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, bundle.ID, bundle.Description, bundle.Revision, bundle.Repository, bundle.SubjectKind, bundle.SubjectIdentity, bundle.Lifecycle, payloads.termination, payloads.selection, payloads.warnings, payloads.deduplicated, payloads.members, bundle.ConcurrencyLimit, bundle.CreatedAt.UTC(), bundle.UpdatedAt.UTC(), nullableTime(bundle.CompletedAt)); err != nil {
		return err
	}
	for _, record := range members {
		if err := requireCurrentReviewSchema(record); err != nil {
			return err
		}
		if err := writeReviewAggregate(tx, record); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Review Bundle write: %w", err)
	}
	return nil
}

func (p bundleProjection) save(bundle model.ReviewBundle) error {
	mutable, err := renderBundleMutablePayload(bundle)
	if err != nil {
		return err
	}
	result, err := p.db.Exec(`UPDATE review_bundles SET lifecycle=?,termination=?,members=?,updated_at=?,completed_at=? WHERE id=?`, bundle.Lifecycle, mutable.termination, mutable.members, bundle.UpdatedAt.UTC(), nullableTime(bundle.CompletedAt), bundle.ID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("save Review Bundle %q: expected one row, updated %d", bundle.ID, updated)
	}
	return nil
}

func renderBundlePayload(bundle model.ReviewBundle) (bundlePayload, error) {
	mutable, err := renderBundleMutablePayload(bundle)
	if err != nil {
		return bundlePayload{}, err
	}
	selection, warnings, deduplicated, err := renderBundleSelectionPayloads(bundle)
	if err != nil {
		return bundlePayload{}, err
	}
	return bundlePayload{members: mutable.members, termination: mutable.termination, selection: selection, warnings: warnings, deduplicated: deduplicated}, nil
}
func renderBundleMutablePayload(bundle model.ReviewBundle) (bundleMutablePayload, error) {
	members, err := json.Marshal(bundle.Members)
	if err != nil {
		return bundleMutablePayload{}, err
	}
	termination, err := json.Marshal(bundle.Termination)
	if err != nil {
		return bundleMutablePayload{}, err
	}
	return bundleMutablePayload{members: members, termination: termination}, nil
}
func renderBundleSelectionPayloads(bundle model.ReviewBundle) ([]byte, []byte, []byte, error) {
	var selection []byte
	if bundle.Selection == nil {
		selection = []byte("null")
	} else {
		var err error
		selection, err = json.Marshal(bundle.Selection)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	warnings := bundle.Warnings
	if warnings == nil {
		warnings = []model.BundleWarning{}
	}
	warningPayload, err := json.Marshal(warnings)
	if err != nil {
		return nil, nil, nil, err
	}
	duplicates := bundle.Deduplicated
	if duplicates == nil {
		duplicates = []model.SkippedDuplicate{}
	}
	duplicatePayload, err := json.Marshal(duplicates)
	if err != nil {
		return nil, nil, nil, err
	}
	return selection, warningPayload, duplicatePayload, nil
}

func (p bundleProjection) load(id model.ReviewBundleID) (model.ReviewBundle, error) {
	var bundle model.ReviewBundle
	var payloads bundlePayload
	var completedAt sql.NullTime
	err := p.db.QueryRow(`SELECT id,description,revision,repository,subject_kind,subject_identity,lifecycle,termination,selection,warnings,deduplicated,members,concurrency_limit,created_at,updated_at,completed_at FROM review_bundles WHERE id=?`, id).Scan(&bundle.ID, &bundle.Description, &bundle.Revision, &bundle.Repository, &bundle.SubjectKind, &bundle.SubjectIdentity, &bundle.Lifecycle, &payloads.termination, &payloads.selection, &payloads.warnings, &payloads.deduplicated, &payloads.members, &bundle.ConcurrencyLimit, &bundle.CreatedAt, &bundle.UpdatedAt, &completedAt)
	if err != nil {
		return model.ReviewBundle{}, notFoundAs(string(id), err)
	}
	if err := decodeBundlePayloads(&bundle, payloads); err != nil {
		return model.ReviewBundle{}, err
	}
	if completedAt.Valid {
		bundle.CompletedAt = completedAt.Time
	}
	return bundle, nil
}
func decodeBundlePayloads(bundle *model.ReviewBundle, payloads bundlePayload) error {
	if err := decodeBundleJSONColumn(payloads.members, "members", &bundle.Members); err != nil {
		return err
	}
	if len(payloads.termination) > 0 && string(payloads.termination) != "null" {
		if err := json.Unmarshal(payloads.termination, &bundle.Termination); err != nil {
			return err
		}
	}
	if err := decodeBundleNullableJSONColumn(payloads.selection, "selection", &bundle.Selection); err != nil {
		return err
	}
	if err := decodeBundleJSONColumn(payloads.warnings, "warnings", &bundle.Warnings); err != nil {
		return err
	}
	return decodeBundleJSONColumn(payloads.deduplicated, "deduplicated", &bundle.Deduplicated)
}
func decodeBundleJSONColumn(payload []byte, name string, destination any) error {
	if len(payload) == 0 || string(payload) == "null" {
		return fmt.Errorf("review bundle %s column holds %q", name, payload)
	}
	return json.Unmarshal(payload, destination)
}
func decodeBundleNullableJSONColumn(payload []byte, name string, destination any) error {
	if string(payload) == "null" {
		return nil
	}
	if len(payload) == 0 {
		return fmt.Errorf("review bundle %s column is empty", name)
	}
	return json.Unmarshal(payload, destination)
}
