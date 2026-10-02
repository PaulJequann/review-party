CREATE TABLE checkpoint_waivers (
  id TEXT PRIMARY KEY,
  checkpoint TEXT NOT NULL,
  content_digest TEXT NOT NULL,
  reason TEXT NOT NULL,
  waived_by TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL
);

CREATE INDEX checkpoint_waivers_key ON checkpoint_waivers(checkpoint, content_digest);
