package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reviewparty/internal/model"
)

const expiredEvidence = `FROM artifacts WHERE (review_id,pass_ordinal,attempt_ordinal) NOT IN (
	SELECT artifacts.review_id,artifacts.pass_ordinal,artifacts.attempt_ordinal FROM artifacts
	JOIN attempts ON attempts.review_id=artifacts.review_id AND attempts.pass_ordinal=artifacts.pass_ordinal AND attempts.ordinal=artifacts.attempt_ordinal
	GROUP BY artifacts.review_id,artifacts.pass_ordinal,artifacts.attempt_ordinal
	ORDER BY MAX(attempts.completed_at) DESC LIMIT ?)`

// ExpireEvidence removes the files before committing the forgotten rows, so a
// failed removal leaves the rows for the next expiry to retry.
func (s *LedgerRecordStore) ExpireEvidence(keep int, remove func([]model.ArtifactReference) error) (returnErr error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin evidence expiry: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(tx))
	}()
	expired, err := queryExpiredEvidence(tx, keep)
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE "+expiredEvidence, keep); err != nil {
		return fmt.Errorf("expire evidence: %w", err)
	}
	if err := remove(expired); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit evidence expiry: %w", err)
	}
	return nil
}

func queryExpiredEvidence(tx *sql.Tx, keep int) (expired []model.ArtifactReference, returnErr error) {
	rows, err := tx.Query("SELECT kind,path,size,digest,truncated "+expiredEvidence, keep)
	if err != nil {
		return nil, fmt.Errorf("read expired evidence: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rows.Close())
	}()
	for rows.Next() {
		var reference model.ArtifactReference
		if err := rows.Scan(&reference.Kind, &reference.Path, &reference.Size, &reference.Digest, &reference.Truncated); err != nil {
			return nil, err
		}
		expired = append(expired, reference)
	}
	return expired, rows.Err()
}

func savedEvidence(tx *sql.Tx, id model.ReviewID) (evidence map[attemptIdentity][]model.ArtifactReference, returnErr error) {
	rows, err := tx.Query(`SELECT attempts.pass_ordinal,attempts.ordinal,artifacts.kind,artifacts.path,artifacts.size,artifacts.digest,artifacts.truncated
		FROM attempts LEFT JOIN artifacts ON artifacts.review_id=attempts.review_id AND artifacts.pass_ordinal=attempts.pass_ordinal AND artifacts.attempt_ordinal=attempts.ordinal
		WHERE attempts.review_id=? ORDER BY attempts.pass_ordinal,attempts.ordinal,artifacts.ordinal`, id)
	if err != nil {
		return nil, fmt.Errorf("read saved evidence: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rows.Close())
	}()
	evidence = map[attemptIdentity][]model.ArtifactReference{}
	for rows.Next() {
		identity := attemptIdentity{reviewID: id}
		var kind, path, digest sql.NullString
		var size sql.NullInt64
		var truncated sql.NullBool
		if err := rows.Scan(&identity.passOrdinal, &identity.attemptOrdinal, &kind, &path, &size, &digest, &truncated); err != nil {
			return nil, err
		}
		references := evidence[identity]
		if kind.Valid {
			references = append(references, model.ArtifactReference{Kind: kind.String, Path: path.String, Size: size.Int64, Digest: digest.String, Truncated: truncated.Bool})
		}
		evidence[identity] = references
	}
	return evidence, rows.Err()
}

func (s *LedgerRecordStore) EvidencePaths() (paths []string, returnErr error) {
	rows, err := s.db.Query("SELECT path FROM artifacts")
	if err != nil {
		return nil, fmt.Errorf("read evidence paths: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rows.Close())
	}()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}
