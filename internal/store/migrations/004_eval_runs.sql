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
  completed_at TIMESTAMP
);
CREATE TABLE eval_runs (
  id TEXT PRIMARY KEY,
  suite_run_id TEXT NOT NULL REFERENCES eval_suite_runs(id),
  case_id TEXT NOT NULL,
  case_schema_version INTEGER NOT NULL,
  case_digest TEXT NOT NULL,
  case_revision BLOB NOT NULL,
  review_id TEXT NOT NULL REFERENCES reviews(id),
  execution_state TEXT NOT NULL,
  adjudication_state TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX eval_runs_case_history ON eval_runs(case_id, created_at DESC);
