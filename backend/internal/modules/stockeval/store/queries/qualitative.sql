-- name: GetQualitative :one
SELECT * FROM stockeval.qualitative WHERE owner_id = @owner_id AND instrument_id = @instrument_id;

-- name: GetQualitativeForUpdate :one
SELECT * FROM stockeval.qualitative WHERE owner_id = @owner_id AND instrument_id = @instrument_id FOR UPDATE;

-- name: UpsertQualitative :exec
INSERT INTO stockeval.qualitative (owner_id, instrument_id, r1, r2, r3, r4, notes, buyback_manual, reviewed_at)
VALUES (@owner_id, @instrument_id, @r1, @r2, @r3, @r4, @notes, @buyback_manual, @reviewed_at)
ON CONFLICT (owner_id, instrument_id) DO UPDATE SET
    r1 = EXCLUDED.r1, r2 = EXCLUDED.r2, r3 = EXCLUDED.r3, r4 = EXCLUDED.r4, notes = EXCLUDED.notes,
    buyback_manual = EXCLUDED.buyback_manual, reviewed_at = EXCLUDED.reviewed_at;
