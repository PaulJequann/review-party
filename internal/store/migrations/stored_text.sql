UPDATE reviews SET subject = CAST(json_remove(subject, '$.patch') AS BLOB)
WHERE json_type(subject, '$.patch') IS NOT NULL;

ALTER TABLE reviews DROP COLUMN result_raw;

ALTER TABLE attempts DROP COLUMN raw_output;

DELETE FROM artifacts WHERE kind NOT IN ('assistant-text', 'reviewer-noise');

DELETE FROM artifacts WHERE EXISTS (
  SELECT 1 FROM attempts JOIN reviews ON reviews.id = attempts.review_id
  WHERE attempts.review_id = artifacts.review_id
    AND attempts.pass_ordinal = artifacts.pass_ordinal
    AND attempts.ordinal = artifacts.attempt_ordinal
    AND attempts.outcome = 'completed'
    AND reviews.lifecycle = 'completed'
);
