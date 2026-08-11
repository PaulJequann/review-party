ALTER TABLE reviews ADD COLUMN replays_review_id TEXT REFERENCES reviews(id);
CREATE INDEX reviews_replay_source ON reviews(replays_review_id, created_at DESC, id DESC);
