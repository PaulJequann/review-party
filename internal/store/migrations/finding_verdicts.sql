CREATE TABLE finding_verdicts (
  seq INTEGER PRIMARY KEY,
  review_id TEXT NOT NULL REFERENCES reviews(id),
  ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
  finding_digest TEXT NOT NULL CHECK (length(finding_digest) = 16),
  verdict TEXT NOT NULL CHECK (verdict IN ('accepted', 'rejected', 'deferred')),
  reason TEXT NOT NULL CHECK (
    length(CAST(reason AS BLOB)) BETWEEN 1 AND 240
    AND instr(reason, char(10)) = 0
    AND instr(reason, char(13)) = 0
  ),
  recorded_by TEXT NOT NULL CHECK (recorded_by <> ''),
  recorded_at TIMESTAMP NOT NULL
);

CREATE INDEX finding_verdicts_finding ON finding_verdicts(review_id, ordinal, seq);
