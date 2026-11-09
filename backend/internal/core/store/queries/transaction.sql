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

-- Registre d'un compte borné par des dates facultatives.
-- name: ListTransactionsBetween :many
SELECT * FROM core.transaction
WHERE owner_id = @owner_id AND account_id = @account_id
  AND (sqlc.narg('from_date')::date IS NULL OR trade_date >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR trade_date <= sqlc.narg('to_date')::date)
ORDER BY trade_date, created_at, id;

-- Écritures en vigueur (ni corrigées ni inverses) touchant un compte, en source ou en contrepartie.
-- name: ListEffectiveForAccount :many
SELECT t.* FROM core.transaction t
WHERE t.owner_id = @owner_id AND t.trade_date <= @as_of
  AND (t.account_id = @account_id OR t.counterpart_account_id = @account_id)
  AND t.reverses_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM core.transaction r WHERE r.owner_id = t.owner_id AND r.reverses_id = t.id)
ORDER BY t.trade_date, t.created_at, t.id;

-- Nombre d'originales de même clé naturelle (D391), pour l'idempotence de l'import (D392).
-- name: CountNaturalKey :one
SELECT count(*) FROM core.transaction
WHERE owner_id = @owner_id AND account_id = @account_id AND reverses_id IS NULL
  AND type = @type AND trade_date = @trade_date
  AND instrument_id IS NOT DISTINCT FROM sqlc.narg('instrument_id')::uuid
  AND quantity IS NOT DISTINCT FROM sqlc.narg('quantity')::numeric
  AND price IS NOT DISTINCT FROM sqlc.narg('price')::numeric
  AND amount IS NOT DISTINCT FROM sqlc.narg('amount')::numeric
  AND split_ratio IS NOT DISTINCT FROM sqlc.narg('split_ratio')::numeric;

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
