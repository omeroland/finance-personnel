-- name: GetSnapshot :one
SELECT * FROM stockeval.snapshot WHERE owner_id = @owner_id AND instrument_id = @instrument_id;

-- name: GetSnapshotForUpdate :one
SELECT * FROM stockeval.snapshot WHERE owner_id = @owner_id AND instrument_id = @instrument_id FOR UPDATE;

-- name: UpsertSnapshot :exec
INSERT INTO stockeval.snapshot (owner_id, instrument_id, price, expected_dividend, lt_debt, equity, debt_to_equity,
    book_value_per_share, pe_imported, payout_ratio_imported, pb_imported, fx_rate, as_of, source)
VALUES (@owner_id, @instrument_id, @price, @expected_dividend, @lt_debt, @equity, @debt_to_equity,
    @book_value_per_share, @pe_imported, @payout_ratio_imported, @pb_imported, @fx_rate, @as_of, @source)
ON CONFLICT (owner_id, instrument_id) DO UPDATE SET
    price = EXCLUDED.price, expected_dividend = EXCLUDED.expected_dividend, lt_debt = EXCLUDED.lt_debt,
    equity = EXCLUDED.equity, debt_to_equity = EXCLUDED.debt_to_equity,
    book_value_per_share = EXCLUDED.book_value_per_share, pe_imported = EXCLUDED.pe_imported,
    payout_ratio_imported = EXCLUDED.payout_ratio_imported, pb_imported = EXCLUDED.pb_imported,
    fx_rate = EXCLUDED.fx_rate, as_of = EXCLUDED.as_of, source = EXCLUDED.source;
