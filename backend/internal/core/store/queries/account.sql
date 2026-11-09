-- name: CreateAccount :one
INSERT INTO core.account (owner_id, name, type, broker, currency)
VALUES (@owner_id, @name, @type, @broker, @currency)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM core.account WHERE owner_id = @owner_id AND id = @id;

-- name: ListAccounts :many
SELECT * FROM core.account WHERE owner_id = @owner_id ORDER BY name;

-- Nom de compte sans égard à la casse (import CSV, D391).
-- name: GetAccountByName :one
SELECT * FROM core.account WHERE owner_id = @owner_id AND lower(name) = lower(@name::text);
