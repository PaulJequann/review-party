CREATE TABLE review_bundles (
  id TEXT PRIMARY KEY,
  party TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  party_revision TEXT NOT NULL,
  repository TEXT NOT NULL,
  subject_kind TEXT NOT NULL,
  subject_identity TEXT NOT NULL,
  lifecycle TEXT NOT NULL,
  termination BLOB,
  members BLOB NOT NULL,
  concurrency_limit INTEGER NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  completed_at TIMESTAMP
);
CREATE INDEX review_bundles_party_history ON review_bundles(party, created_at DESC);
