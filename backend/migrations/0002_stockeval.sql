-- +goose Up
CREATE SCHEMA stockeval;

CREATE TABLE stockeval.watchlist (
    owner_id      uuid NOT NULL,
    instrument_id uuid NOT NULL REFERENCES core.instrument (id),
    added_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, instrument_id)
);

CREATE TABLE stockeval.annual_fact (
    owner_id      uuid NOT NULL,
    instrument_id uuid NOT NULL REFERENCES core.instrument (id),
    fiscal_year   integer NOT NULL CHECK (fiscal_year BETWEEN 1900 AND 2200),
    metric        text NOT NULL CHECK (metric IN ('eps_diluted', 'dps', 'dividend_yield', 'shares_diluted')),
    value         numeric(20,6) NOT NULL,
    currency      char(3),
    source        text NOT NULL CHECK (source IN ('manual', 'xlsx', 'provider')),
    fetched_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, instrument_id, fiscal_year, metric)
);

CREATE TABLE stockeval.fact_conflict (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        uuid NOT NULL,
    instrument_id   uuid NOT NULL REFERENCES core.instrument (id),
    fiscal_year     integer NOT NULL,
    metric          text NOT NULL CHECK (metric IN ('eps_diluted', 'dps', 'dividend_yield', 'shares_diluted')),
    existing_value  numeric(20,6) NOT NULL,
    existing_source text NOT NULL CHECK (existing_source IN ('manual', 'xlsx', 'provider')),
    incoming_value  numeric(20,6) NOT NULL,
    incoming_source text NOT NULL CHECK (incoming_source IN ('manual', 'xlsx', 'provider')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    resolved_at     timestamptz
);
CREATE INDEX fact_conflict_open_idx ON stockeval.fact_conflict (owner_id, instrument_id) WHERE resolved_at IS NULL;

CREATE TABLE stockeval.snapshot (
    owner_id              uuid NOT NULL,
    instrument_id         uuid NOT NULL REFERENCES core.instrument (id),
    price                 numeric(20,6),
    expected_dividend     numeric(20,6),
    lt_debt               numeric(20,6),
    equity                numeric(20,6),
    debt_to_equity        numeric(20,6), -- fraction ; seulement si seul le ratio est connu
    book_value_per_share  numeric(20,6),
    pe_imported           numeric(20,6),
    payout_ratio_imported numeric(20,6), -- fraction (0.70 = 70 %)
    pb_imported           numeric(20,6),
    fx_rate               numeric(20,6),
    as_of                 timestamptz NOT NULL,
    source                text NOT NULL CHECK (source IN ('manual', 'xlsx', 'provider')),
    PRIMARY KEY (owner_id, instrument_id)
);

CREATE TABLE stockeval.qualitative (
    owner_id       uuid NOT NULL,
    instrument_id  uuid NOT NULL REFERENCES core.instrument (id),
    r1             boolean,
    r2             boolean,
    r3             boolean,
    r4             boolean,
    notes          jsonb NOT NULL DEFAULT '{}'::jsonb,
    buyback_manual boolean,
    reviewed_at    timestamptz,
    PRIMARY KEY (owner_id, instrument_id)
);

-- Archive immuable : le rôle applicatif n'a que SELECT et INSERT (migration 0003).
CREATE TABLE stockeval.evaluation (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id      uuid NOT NULL,
    instrument_id uuid NOT NULL REFERENCES core.instrument (id),
    mode          text NOT NULL,
    rules_version text NOT NULL,
    rules_hash    text NOT NULL,
    inputs_hash   text NOT NULL,
    inputs        jsonb NOT NULL,
    result        jsonb NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX evaluation_owner_instrument_idx ON stockeval.evaluation (owner_id, instrument_id, created_at DESC);

CREATE TABLE stockeval.import_job (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL,
    type        text NOT NULL CHECK (type IN ('xlsx', 'provider')),
    status      text NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    report      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

-- +goose Down
DROP TABLE stockeval.import_job;
DROP TABLE stockeval.evaluation;
DROP TABLE stockeval.qualitative;
DROP TABLE stockeval.snapshot;
DROP TABLE stockeval.fact_conflict;
DROP TABLE stockeval.annual_fact;
DROP TABLE stockeval.watchlist;
DROP SCHEMA stockeval;
