-- name: GetInstrument :one
SELECT * FROM core.instrument WHERE owner_id = @owner_id AND id = @id;

-- name: GetInstrumentBySymbol :one
SELECT * FROM core.instrument WHERE owner_id = @owner_id AND symbol = @symbol;

-- name: InsertInstrument :one
INSERT INTO core.instrument (owner_id, symbol, name, exchange, kind, quote_currency, report_currency, is_financial)
VALUES (@owner_id, @symbol, @name, @exchange, @kind, @quote_currency, @report_currency, @is_financial)
RETURNING *;
