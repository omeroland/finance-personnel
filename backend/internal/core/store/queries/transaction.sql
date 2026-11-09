-- name: InsertTransaction :one
INSERT INTO core.transaction (
    owner_id, account_id, type, trade_date, instrument_id, quantity, price, amount, fee,
    split_ratio, counterpart_account_id, currency, reverses_id, note, source)
VALUES (
    @owner_id, @account_id, @type, @trade_date, @instrument_id, @quantity, @price, @amount, @fee,
    @split_ratio, @counterpart_account_id, @currency, @reverses_id, @note, @source)
RETURNING *;

-- name: GetTransaction :one
SELECT * FROM core.transaction WHERE owner_id = @owner_id AND id = @id;

-- name: ListTransactions :many
SELECT * FROM core.transaction
WHERE owner_id = @owner_id AND account_id = @account_id
ORDER BY trade_date, created_at, id;

-- Positions d'un compte à une date, dérivées du registre (jamais stockées).
-- name: ListPositionsAt :many
SELECT l.account_id, l.instrument_id, i.symbol,
       core.fold_position(l.kind, l.delta, l.split_ratio ORDER BY l.trade_date, l.created_at, l.id)::numeric AS quantity
FROM core.position_ledger l
JOIN core.instrument i ON i.owner_id = l.owner_id AND i.id = l.instrument_id
WHERE l.owner_id = @owner_id AND l.account_id = @account_id AND l.trade_date <= @as_of
GROUP BY l.account_id, l.instrument_id, i.symbol
HAVING core.fold_position(l.kind, l.delta, l.split_ratio ORDER BY l.trade_date, l.created_at, l.id) <> 0
ORDER BY i.symbol;
