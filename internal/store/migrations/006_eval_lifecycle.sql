ALTER TABLE eval_suite_runs ADD COLUMN lifecycle TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE eval_suite_runs ADD COLUMN termination BLOB;
UPDATE eval_suite_runs
SET lifecycle = CASE WHEN completed_at IS NULL THEN 'incomplete' ELSE 'completed' END;

ALTER TABLE eval_runs RENAME TO eval_runs_v5;
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
INSERT INTO eval_runs(
  id, suite_run_id, case_id, case_schema_version, case_digest, case_revision,
  review_id, execution_state, adjudication_state, created_at, updated_at
)
SELECT id, suite_run_id, case_id, case_schema_version, case_digest, case_revision,
       review_id, execution_state, adjudication_state, created_at, created_at
FROM eval_runs_v5;
DROP TABLE eval_runs_v5;
CREATE INDEX eval_runs_case_history ON eval_runs(case_id, created_at DESC);
