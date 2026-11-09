-- name: CreateStatement :one
INSERT INTO core.statement (owner_id, account_id, month, end_value, contribution)
VALUES (@owner_id, @account_id, @month, @end_value, @contribution)
RETURNING *;

-- name: ListStatements :many
SELECT * FROM core.statement
WHERE owner_id = @owner_id AND account_id = @account_id
ORDER BY month;
