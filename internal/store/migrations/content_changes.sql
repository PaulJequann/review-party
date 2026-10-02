CREATE TABLE review_content_changes (
  review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  before_blob TEXT NOT NULL,
  after_blob TEXT NOT NULL,
  PRIMARY KEY (review_id, path)
);

CREATE INDEX review_content_changes_entry ON review_content_changes(path, before_blob, after_blob);
