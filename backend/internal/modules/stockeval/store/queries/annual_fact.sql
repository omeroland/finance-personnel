-- name: GetAnnualFactForUpdate :one
SELECT * FROM stockeval.annual_fact
WHERE owner_id = @owner_id AND instrument_id = @instrument_id AND fiscal_year = @fiscal_year AND metric = @metric
FOR UPDATE;

-- name: ListAnnualFacts :many
SELECT * FROM stockeval.annual_fact
WHERE owner_id = @owner_id AND instrument_id = @instrument_id
ORDER BY metric, fiscal_year;

-- name: UpsertAnnualFact :exec
INSERT INTO stockeval.annual_fact (owner_id, instrument_id, fiscal_year, metric, value, currency, source)
VALUES (@owner_id, @instrument_id, @fiscal_year, @metric, @value, @currency, @source)
ON CONFLICT (owner_id, instrument_id, fiscal_year, metric)
DO UPDATE SET value = EXCLUDED.value, currency = EXCLUDED.currency, source = EXCLUDED.source, fetched_at = now();
