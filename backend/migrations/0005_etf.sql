-- +goose Up
CREATE SCHEMA etf;

-- Profil d'un FNB, daté (D322) : une ligne par date de répartition ; la dernière date est le profil courant.
-- Ratios en fraction, double precision (le contrat est format: double).
CREATE TABLE etf.etf_profile (
    owner_id      uuid NOT NULL,
    instrument_id uuid NOT NULL,
    as_of         date NOT NULL,
    type          text NOT NULL CHECK (type IN ('equity', 'fixed_income', 'balanced', 'other')),
    region        text CHECK (region IN ('ca', 'us', 'intl', 'global')),
    equity_ca     double precision NOT NULL DEFAULT 0 CHECK (equity_ca BETWEEN 0 AND 1),
    equity_us     double precision NOT NULL DEFAULT 0 CHECK (equity_us BETWEEN 0 AND 1),
    equity_intl   double precision NOT NULL DEFAULT 0 CHECK (equity_intl BETWEEN 0 AND 1),
    bond_ca       double precision NOT NULL DEFAULT 0 CHECK (bond_ca BETWEEN 0 AND 1),
    bond_other    double precision NOT NULL DEFAULT 0 CHECK (bond_other BETWEEN 0 AND 1),
    other_assets  double precision NOT NULL DEFAULT 0 CHECK (other_assets BETWEEN 0 AND 1),
    mer           double precision CHECK (mer BETWEEN 0 AND 1),
    size          numeric(20,6) CHECK (size >= 0),
    esg           boolean,
    return_1y     double precision,
    return_2y     double precision,
    return_3y     double precision,
    return_4y     double precision,
    return_5y     double precision,
    return_6y     double precision,
    return_7y     double precision,
    return_8y     double precision,
    return_9y     double precision,
    return_10y    double precision,
    source        text NOT NULL CHECK (source IN ('manual', 'xlsx', 'provider')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, instrument_id, as_of),
    FOREIGN KEY (owner_id, instrument_id) REFERENCES core.instrument (owner_id, id),
    CHECK (abs(equity_ca + equity_us + equity_intl + bond_ca + bond_other + other_assets - 1) <= 0.0001)
);

CREATE TABLE etf.model (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, id),
    UNIQUE (owner_id, name)
);

-- Chaque modification publie une nouvelle version (D313) ; une version ne change plus.
CREATE TABLE etf.model_version (
    owner_id         uuid NOT NULL,
    model_id         uuid NOT NULL,
    version          integer NOT NULL CHECK (version >= 1),
    equity_ca        double precision NOT NULL DEFAULT 0 CHECK (equity_ca BETWEEN 0 AND 1),
    equity_us        double precision NOT NULL DEFAULT 0 CHECK (equity_us BETWEEN 0 AND 1),
    equity_intl      double precision NOT NULL DEFAULT 0 CHECK (equity_intl BETWEEN 0 AND 1),
    bond_ca          double precision NOT NULL DEFAULT 0 CHECK (bond_ca BETWEEN 0 AND 1),
    bond_other       double precision NOT NULL DEFAULT 0 CHECK (bond_other BETWEEN 0 AND 1),
    other_assets     double precision NOT NULL DEFAULT 0 CHECK (other_assets BETWEEN 0 AND 1),
    cash_target      double precision NOT NULL DEFAULT 0 CHECK (cash_target BETWEEN 0 AND 1),
    band_relative    double precision NOT NULL DEFAULT 0.20 CHECK (band_relative BETWEEN 0 AND 1),
    band_cash_points double precision NOT NULL DEFAULT 0.05 CHECK (band_cash_points BETWEEN 0 AND 1),
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, model_id, version),
    FOREIGN KEY (owner_id, model_id) REFERENCES etf.model (owner_id, id),
    CHECK (abs(equity_ca + equity_us + equity_intl + bond_ca + bond_other + other_assets + cash_target - 1) <= 0.0001)
);

-- Fonds retenus : 20 au plus (position de 1 à 20). Poids + encaisse = 1 est vérifié à l'écriture par l'API.
CREATE TABLE etf.model_holding (
    owner_id      uuid NOT NULL,
    model_id      uuid NOT NULL,
    version       integer NOT NULL,
    position      smallint NOT NULL CHECK (position BETWEEN 1 AND 20),
    instrument_id uuid NOT NULL,
    weight        double precision NOT NULL CHECK (weight BETWEEN 0 AND 1),
    PRIMARY KEY (owner_id, model_id, version, position),
    UNIQUE (owner_id, model_id, version, instrument_id),
    FOREIGN KEY (owner_id, model_id, version) REFERENCES etf.model_version (owner_id, model_id, version),
    FOREIGN KEY (owner_id, instrument_id) REFERENCES core.instrument (owner_id, id)
);

-- +goose Down
DROP TABLE etf.model_holding;
DROP TABLE etf.model_version;
DROP TABLE etf.model;
DROP TABLE etf.etf_profile;
DROP SCHEMA etf;
