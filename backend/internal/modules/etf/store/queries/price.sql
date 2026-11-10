-- Dernier prix d'écriture (achat, vente ou réinvestissement) de chaque symbole demandé, dans la devise
-- demandée, parmi les écritures en vigueur (ni corrigées ni inverses). Aucun prix de marché n'est stocké
-- en phase 2 (D402, D410).
-- name: LastTradePrices :many
SELECT DISTINCT ON (t.instrument_id) i.symbol, t.price::numeric AS price, t.trade_date
FROM core.transaction t
JOIN core.instrument i ON i.owner_id = t.owner_id AND i.id = t.instrument_id
WHERE t.owner_id = @owner_id
  AND t.type IN ('buy', 'sell', 'drip')
  AND t.price > 0
  AND t.currency = @currency::text
  AND t.trade_date <= @as_of::date
  AND i.symbol = ANY(@symbols::text[])
  AND t.reverses_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM core.transaction r WHERE r.owner_id = t.owner_id AND r.reverses_id = t.id)
ORDER BY t.instrument_id, t.trade_date DESC, t.created_at DESC, t.id DESC;
