-- name: ListWatchlist :many
SELECT i.*
FROM stockeval.watchlist w
JOIN core.instrument i ON i.id = w.instrument_id AND i.owner_id = w.owner_id
WHERE w.owner_id = @owner_id
ORDER BY i.symbol;

-- name: IsOnWatchlist :one
SELECT EXISTS (
    SELECT 1 FROM stockeval.watchlist WHERE owner_id = @owner_id AND instrument_id = @instrument_id
);

-- name: AddToWatchlist :exec
INSERT INTO stockeval.watchlist (owner_id, instrument_id)
VALUES (@owner_id, @instrument_id)
ON CONFLICT (owner_id, instrument_id) DO NOTHING;

-- name: RemoveFromWatchlist :execrows
DELETE FROM stockeval.watchlist WHERE owner_id = @owner_id AND instrument_id = @instrument_id;
