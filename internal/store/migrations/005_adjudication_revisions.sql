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
