CREATE TABLE reviews (
  id TEXT PRIMARY KEY,
  lifecycle TEXT NOT NULL,
  subject BLOB NOT NULL,
  profile_revision BLOB NOT NULL,
  profile_snapshot BLOB NOT NULL,
  result_status TEXT,
  result_summary TEXT,
  result_raw TEXT,
  result_finding_count INTEGER NOT NULL DEFAULT 0,
  termination BLOB,
  runtime BLOB,
  timings BLOB,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  replays_review_id TEXT REFERENCES reviews(id)
);
CREATE INDEX reviews_history_order ON reviews(created_at DESC, id DESC);
CREATE INDEX reviews_history_reviewer ON reviews(json_extract(profile_revision, '$.reviewer_id'), created_at DESC, id DESC);
CREATE INDEX reviews_replay_source ON reviews(replays_review_id, created_at DESC, id DESC);

CREATE TABLE passes (
  review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  name TEXT NOT NULL,
  required BOOLEAN NOT NULL,
  PRIMARY KEY(review_id, ordinal)
);
CREATE TABLE attempts (
  review_id TEXT NOT NULL,
  pass_ordinal INTEGER NOT NULL,
  ordinal INTEGER NOT NULL,
  number INTEGER NOT NULL,
  outcome TEXT NOT NULL,
  provenance BLOB NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  raw_output TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMP NOT NULL,
  completed_at TIMESTAMP NOT NULL,
  retry_after_ms INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(review_id, pass_ordinal, ordinal),
  FOREIGN KEY(review_id, pass_ordinal) REFERENCES passes(review_id, ordinal) ON DELETE CASCADE
);
CREATE TABLE findings (
  review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  severity TEXT NOT NULL,
  category TEXT NOT NULL,
  location TEXT NOT NULL,
  failure TEXT NOT NULL,
  evidence TEXT NOT NULL,
  fix TEXT NOT NULL,
  test TEXT NOT NULL,
  PRIMARY KEY(review_id, ordinal)
);
CREATE TABLE artifacts (
  review_id TEXT NOT NULL,
  pass_ordinal INTEGER NOT NULL,
  attempt_ordinal INTEGER NOT NULL,
  ordinal INTEGER NOT NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  size INTEGER NOT NULL,
  digest TEXT NOT NULL,
  truncated BOOLEAN NOT NULL,
  PRIMARY KEY(review_id, pass_ordinal, attempt_ordinal, ordinal),
  FOREIGN KEY(review_id, pass_ordinal, attempt_ordinal) REFERENCES attempts(review_id, pass_ordinal, ordinal) ON DELETE CASCADE
);

CREATE TABLE eval_suite_runs (
  id TEXT PRIMARY KEY,
  suite TEXT NOT NULL,
  suite_revision TEXT NOT NULL,
  suite_digest TEXT NOT NULL,
  experiment BLOB NOT NULL,
  eval_run_ids BLOB NOT NULL,
  completed_clean_count INTEGER NOT NULL DEFAULT 0,
  completed_findings_count INTEGER NOT NULL DEFAULT 0,
  incomplete_count INTEGER NOT NULL DEFAULT 0,
  started_at TIMESTAMP NOT NULL,
  completed_at TIMESTAMP,
  lifecycle TEXT NOT NULL DEFAULT 'pending',
  termination BLOB
);
CREATE TABLE eval_runs (
  id TEXT PRIMARY KEY,
  suite_run_id TEXT NOT NULL REFERENCES eval_suite_runs(id),
  case_id TEXT NOT NULL,
  case_schema_version INTEGER NOT NULL,
  case_digest TEXT NOT NULL,
  case_revision BLOB NOT NULL,
  review_id TEXT REFERENCES reviews(id),
  execution_state TEXT NOT NULL,
  adjudication_state TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX eval_runs_case_history ON eval_runs(case_id, created_at DESC);

CREATE TABLE adjudication_revisions (
  id TEXT PRIMARY KEY,
  suite_run_id TEXT NOT NULL REFERENCES eval_suite_runs(id),
  revision_number INTEGER NOT NULL,
  document BLOB NOT NULL,
  score BLOB NOT NULL,
  created_at TIMESTAMP NOT NULL,
  UNIQUE(suite_run_id, revision_number)
);
CREATE INDEX adjudication_suite_history ON adjudication_revisions(suite_run_id, revision_number DESC);

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
