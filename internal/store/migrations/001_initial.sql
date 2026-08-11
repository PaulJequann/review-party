CREATE TABLE reviews (
  id TEXT PRIMARY KEY, lifecycle TEXT NOT NULL, subject BLOB NOT NULL,
  profile_revision BLOB NOT NULL, profile_snapshot BLOB NOT NULL,
  result_status TEXT, result_summary TEXT, result_raw TEXT,
  result_finding_count INTEGER NOT NULL DEFAULT 0, termination BLOB,
  runtime BLOB, timings BLOB, incomplete_cause TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL
);
CREATE TABLE passes (review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE, ordinal INTEGER NOT NULL, name TEXT NOT NULL, required BOOLEAN NOT NULL, PRIMARY KEY(review_id, ordinal));
CREATE TABLE attempts (review_id TEXT NOT NULL, pass_ordinal INTEGER NOT NULL, ordinal INTEGER NOT NULL, number INTEGER NOT NULL, outcome TEXT NOT NULL, provenance BLOB NOT NULL, diagnostic TEXT NOT NULL DEFAULT '', raw_output TEXT NOT NULL DEFAULT '', started_at TIMESTAMP NOT NULL, completed_at TIMESTAMP NOT NULL, PRIMARY KEY(review_id, pass_ordinal, ordinal), FOREIGN KEY(review_id, pass_ordinal) REFERENCES passes(review_id, ordinal) ON DELETE CASCADE);
CREATE TABLE findings (review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE, ordinal INTEGER NOT NULL, severity TEXT NOT NULL, category TEXT NOT NULL, location TEXT NOT NULL, failure TEXT NOT NULL, evidence TEXT NOT NULL, fix TEXT NOT NULL, test TEXT NOT NULL, PRIMARY KEY(review_id, ordinal));
CREATE TABLE artifacts (review_id TEXT NOT NULL, pass_ordinal INTEGER NOT NULL, attempt_ordinal INTEGER NOT NULL, ordinal INTEGER NOT NULL, kind TEXT NOT NULL, path TEXT NOT NULL, size INTEGER NOT NULL, digest TEXT NOT NULL, truncated BOOLEAN NOT NULL, PRIMARY KEY(review_id, pass_ordinal, attempt_ordinal, ordinal), FOREIGN KEY(review_id, pass_ordinal, attempt_ordinal) REFERENCES attempts(review_id, pass_ordinal, ordinal) ON DELETE CASCADE);
