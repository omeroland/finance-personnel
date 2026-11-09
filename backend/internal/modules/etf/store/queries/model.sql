-- name: CreateModel :one
INSERT INTO etf.model (owner_id, name) VALUES (@owner_id, @name) RETURNING *;

-- name: GetModel :one
SELECT * FROM etf.model WHERE owner_id = @owner_id AND id = @id;

-- name: GetModelByName :one
SELECT * FROM etf.model WHERE owner_id = @owner_id AND name = @name;

-- name: ListModels :many
SELECT * FROM etf.model WHERE owner_id = @owner_id ORDER BY name;

-- Publie la version suivante (D313). Un conflit de clé signale une écriture concurrente : réessayer.
-- name: InsertModelVersion :one
INSERT INTO etf.model_version (
    owner_id, model_id, version, equity_ca, equity_us, equity_intl, bond_ca, bond_other, other_assets,
    cash_target, band_relative, band_cash_points)
SELECT @owner_id, @model_id, COALESCE(MAX(v.version), 0) + 1,
       @equity_ca, @equity_us, @equity_intl, @bond_ca, @bond_other, @other_assets,
       @cash_target, @band_relative, @band_cash_points
FROM (SELECT 1) one
LEFT JOIN etf.model_version v ON v.owner_id = @owner_id AND v.model_id = @model_id
RETURNING *;

-- name: InsertModelHolding :exec
INSERT INTO etf.model_holding (owner_id, model_id, version, position, instrument_id, weight)
VALUES (@owner_id, @model_id, @version, @position, @instrument_id, @weight);

-- name: GetLatestModelVersion :one
SELECT * FROM etf.model_version
WHERE owner_id = @owner_id AND model_id = @model_id
ORDER BY version DESC LIMIT 1;

-- name: ListModelHoldings :many
SELECT h.*, i.symbol
FROM etf.model_holding h
JOIN core.instrument i ON i.owner_id = h.owner_id AND i.id = h.instrument_id
WHERE h.owner_id = @owner_id AND h.model_id = @model_id AND h.version = @version
ORDER BY h.position;
