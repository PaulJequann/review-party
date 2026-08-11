CREATE INDEX reviews_history_order ON reviews(created_at DESC, id DESC);
CREATE INDEX reviews_history_reviewer ON reviews(json_extract(profile_revision, '$.reviewer_id'), created_at DESC, id DESC);
