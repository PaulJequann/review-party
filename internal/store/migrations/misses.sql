CREATE TABLE misses (
  id TEXT PRIMARY KEY,
  review_id TEXT NOT NULL REFERENCES reviews(id),
  path TEXT NOT NULL,
  line INTEGER,
  source TEXT NOT NULL,
  description TEXT NOT NULL,
  recorded_by TEXT NOT NULL,
  recorded_at TIMESTAMP NOT NULL,
  removed_at TIMESTAMP,
  removed_by TEXT,
  removal_reason TEXT,
  CHECK ((removed_at IS NULL) = (removed_by IS NULL) AND (removed_at IS NULL) = (removal_reason IS NULL))
);

CREATE INDEX misses_review ON misses(review_id, recorded_at);
