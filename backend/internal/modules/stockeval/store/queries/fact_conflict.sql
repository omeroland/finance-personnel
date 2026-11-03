-- name: FindOpenConflict :one
SELECT * FROM stockeval.fact_conflict
WHERE owner_id = @owner_id AND instrument_id = @instrument_id AND fiscal_year = @fiscal_year AND metric = @metric
  AND incoming_value = @incoming_value AND incoming_source = @incoming_source
  AND existing_value = @existing_value AND existing_source = @existing_source
  AND resolved_at IS NULL;

-- name: InsertFactConflict :one
INSERT INTO stockeval.fact_conflict
    (owner_id, instrument_id, fiscal_year, metric, existing_value, existing_source, incoming_value, incoming_source)
VALUES (@owner_id, @instrument_id, @fiscal_year, @metric, @existing_value, @existing_source, @incoming_value, @incoming_source)
RETURNING *;

-- name: ListOpenConflicts :many
SELECT * FROM stockeval.fact_conflict
WHERE owner_id = @owner_id AND instrument_id = @instrument_id AND resolved_at IS NULL
ORDER BY created_at, id;
