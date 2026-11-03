-- name: InsertEvaluation :one
INSERT INTO stockeval.evaluation (owner_id, instrument_id, mode, rules_version, rules_hash, inputs_hash, inputs, result)
VALUES (@owner_id, @instrument_id, @mode, @rules_version, @rules_hash, @inputs_hash, @inputs, @result)
RETURNING *;

-- name: GetEvaluation :one
SELECT * FROM stockeval.evaluation WHERE owner_id = @owner_id AND id = @id;

-- name: ListEvaluations :many
SELECT * FROM stockeval.evaluation
WHERE owner_id = @owner_id AND instrument_id = @instrument_id
ORDER BY created_at DESC, id
LIMIT @max_rows;
