-- name: UpsertEtfProfile :exec
INSERT INTO etf.etf_profile (
    owner_id, instrument_id, as_of, type, region,
    equity_ca, equity_us, equity_intl, bond_ca, bond_other, other_assets,
    mer, size, esg,
    return_1y, return_2y, return_3y, return_4y, return_5y, return_6y, return_7y, return_8y, return_9y, return_10y,
    source)
VALUES (
    @owner_id, @instrument_id, @as_of, @type, @region,
    @equity_ca, @equity_us, @equity_intl, @bond_ca, @bond_other, @other_assets,
    @mer, @size, @esg,
    @return_1y, @return_2y, @return_3y, @return_4y, @return_5y, @return_6y, @return_7y, @return_8y, @return_9y, @return_10y,
    @source)
ON CONFLICT (owner_id, instrument_id, as_of) DO UPDATE SET
    type = EXCLUDED.type, region = EXCLUDED.region,
    equity_ca = EXCLUDED.equity_ca, equity_us = EXCLUDED.equity_us, equity_intl = EXCLUDED.equity_intl,
    bond_ca = EXCLUDED.bond_ca, bond_other = EXCLUDED.bond_other, other_assets = EXCLUDED.other_assets,
    mer = EXCLUDED.mer, size = EXCLUDED.size, esg = EXCLUDED.esg,
    return_1y = EXCLUDED.return_1y, return_2y = EXCLUDED.return_2y, return_3y = EXCLUDED.return_3y,
    return_4y = EXCLUDED.return_4y, return_5y = EXCLUDED.return_5y, return_6y = EXCLUDED.return_6y,
    return_7y = EXCLUDED.return_7y, return_8y = EXCLUDED.return_8y, return_9y = EXCLUDED.return_9y,
    return_10y = EXCLUDED.return_10y, source = EXCLUDED.source;

-- Dernier profil de chaque FNB, filtre optionnel sur le RFG maximal (fraction).
-- name: ListLatestEtfProfiles :many
SELECT p.*, i.symbol, i.name, i.quote_currency
FROM core.instrument i
JOIN LATERAL (
    SELECT * FROM etf.etf_profile e
    WHERE e.owner_id = i.owner_id AND e.instrument_id = i.id
    ORDER BY e.as_of DESC LIMIT 1
) p ON true
WHERE i.owner_id = @owner_id AND i.kind = 'etf'
  AND (sqlc.narg('max_mer')::double precision IS NULL OR p.mer <= sqlc.narg('max_mer')::double precision)
ORDER BY i.symbol;

-- name: ListEtfProfileHistory :many
SELECT * FROM etf.etf_profile
WHERE owner_id = @owner_id AND instrument_id = @instrument_id
ORDER BY as_of DESC;
