-- +goose Up
CREATE SCHEMA core;

-- Instruments partagés par les modules (D30). owner_id partout (D1) : le symbole
-- est unique par propriétaire (D40).
CREATE TABLE core.instrument (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        uuid NOT NULL,
    symbol          text NOT NULL CHECK (symbol <> ''),
    name            text NOT NULL,
    exchange        text NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('stock', 'etf', 'cash')),
    quote_currency  char(3) NOT NULL,
    report_currency char(3) NOT NULL,
    is_financial    boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, symbol)
);

-- Journal d'audit : insertion seule pour le rôle applicatif.
CREATE TABLE core.audit_log (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id uuid NOT NULL,
    actor    text NOT NULL,
    action   text NOT NULL,
    entity   text NOT NULL,
    before   jsonb,
    after    jsonb,
    at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_owner_at_idx ON core.audit_log (owner_id, at DESC);

-- +goose Down
DROP TABLE core.audit_log;
DROP TABLE core.instrument;
DROP SCHEMA core;
