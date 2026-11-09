-- +goose Up
-- Socle de la phase 2 : comptes, registre en insertion seule, relevés (D320 à D325).
-- Clé (owner_id, id) sur instrument : permet des clés étrangères composites, donc
-- l'impossibilité de relier deux propriétaires différents (D1).
ALTER TABLE core.instrument ADD CONSTRAINT instrument_owner_id_id_key UNIQUE (owner_id, id);

CREATE TABLE core.account (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    type       text NOT NULL CHECK (type IN ('celi', 'reer', 'celiapp', 'reee', 'taxable')),
    broker     text NOT NULL CHECK (char_length(broker) BETWEEN 1 AND 100),
    currency   char(3) NOT NULL CHECK (currency IN ('CAD', 'USD')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, id),
    UNIQUE (owner_id, name)
);

-- Registre en insertion seule (D310). Les montants, quantités et prix sont saisis positifs ;
-- le sens vient du type. Une correction est une écriture inverse (reverses_id) qui recopie
-- l'originale ; positions et rendements ignorent l'originale et son inverse.
CREATE TABLE core.transaction (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id               uuid NOT NULL,
    account_id             uuid NOT NULL,
    type                   text NOT NULL CHECK (type IN
                               ('buy', 'sell', 'dividend', 'drip', 'contribution', 'withdrawal', 'fee', 'transfer', 'split')),
    trade_date             date NOT NULL,
    instrument_id          uuid,
    quantity               numeric(20,6) CHECK (quantity > 0),
    price                  numeric(20,6) CHECK (price >= 0),
    amount                 numeric(20,6) CHECK (amount >= 0),
    fee                    numeric(20,6) CHECK (fee >= 0),
    split_ratio            numeric(20,6) CHECK (split_ratio > 0),
    counterpart_account_id uuid,
    currency               char(3) NOT NULL CHECK (currency IN ('CAD', 'USD')),
    reverses_id            uuid,
    note                   text CHECK (char_length(note) <= 500),
    source                 text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'csv', 'xlsx', 'provider')),
    created_at             timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, id),
    UNIQUE (owner_id, reverses_id),
    FOREIGN KEY (owner_id, account_id) REFERENCES core.account (owner_id, id),
    FOREIGN KEY (owner_id, instrument_id) REFERENCES core.instrument (owner_id, id),
    FOREIGN KEY (owner_id, counterpart_account_id) REFERENCES core.account (owner_id, id),
    FOREIGN KEY (owner_id, reverses_id) REFERENCES core.transaction (owner_id, id),
    CHECK (counterpart_account_id IS NULL OR counterpart_account_id <> account_id),
    CHECK (CASE type
        WHEN 'buy' THEN instrument_id IS NOT NULL AND quantity IS NOT NULL AND price IS NOT NULL
        WHEN 'sell' THEN instrument_id IS NOT NULL AND quantity IS NOT NULL AND price IS NOT NULL
        WHEN 'drip' THEN instrument_id IS NOT NULL AND quantity IS NOT NULL AND price IS NOT NULL
        WHEN 'dividend' THEN instrument_id IS NOT NULL AND amount IS NOT NULL
        WHEN 'split' THEN instrument_id IS NOT NULL AND split_ratio IS NOT NULL
        WHEN 'transfer' THEN counterpart_account_id IS NOT NULL
                             AND (amount IS NOT NULL OR (instrument_id IS NOT NULL AND quantity IS NOT NULL))
        ELSE amount IS NOT NULL
    END)
);
CREATE INDEX transaction_account_date_idx ON core.transaction (owner_id, account_id, trade_date, created_at);
CREATE INDEX transaction_counterpart_idx ON core.transaction (owner_id, counterpart_account_id)
    WHERE counterpart_account_id IS NOT NULL;

-- Refus de UPDATE, DELETE et TRUNCATE pour tout rôle, propriétaire des tables compris.
-- +goose StatementBegin
CREATE FUNCTION core.transaction_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'core.transaction est en insertion seule (% refusé) : corriger par une écriture inverse', TG_OP
        USING ERRCODE = 'integrity_constraint_violation';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER transaction_no_update_delete BEFORE UPDATE OR DELETE ON core.transaction
    FOR EACH ROW EXECUTE FUNCTION core.transaction_immutable();
CREATE TRIGGER transaction_no_truncate BEFORE TRUNCATE ON core.transaction
    FOR EACH STATEMENT EXECUTE FUNCTION core.transaction_immutable();

-- Une écriture inverse recopie l'originale, qui ne peut pas être elle-même une inverse.
-- +goose StatementBegin
CREATE FUNCTION core.transaction_check_reversal() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    orig core.transaction%ROWTYPE;
BEGIN
    IF NEW.reverses_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT * INTO orig FROM core.transaction WHERE owner_id = NEW.owner_id AND id = NEW.reverses_id;
    IF orig.reverses_id IS NOT NULL THEN
        RAISE EXCEPTION 'une écriture inverse ne peut pas être inversée' USING ERRCODE = 'check_violation';
    END IF;
    IF (NEW.account_id, NEW.type, NEW.currency) IS DISTINCT FROM (orig.account_id, orig.type, orig.currency)
       OR (NEW.instrument_id, NEW.quantity, NEW.price, NEW.amount, NEW.fee, NEW.split_ratio, NEW.counterpart_account_id)
          IS DISTINCT FROM
          (orig.instrument_id, orig.quantity, orig.price, orig.amount, orig.fee, orig.split_ratio, orig.counterpart_account_id)
    THEN
        RAISE EXCEPTION 'l''écriture inverse doit recopier l''originale' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER transaction_reversal_check BEFORE INSERT ON core.transaction
    FOR EACH ROW EXECUTE FUNCTION core.transaction_check_reversal();

-- Relevé de valeur de fin de mois (D310) : un par compte et par mois.
CREATE TABLE core.statement (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid NOT NULL,
    account_id   uuid NOT NULL,
    month        text NOT NULL CHECK (month ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    end_value    numeric(20,6) NOT NULL CHECK (end_value >= 0),
    contribution numeric(20,6),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, account_id, month),
    FOREIGN KEY (owner_id, account_id) REFERENCES core.account (owner_id, id)
);

-- Positions : jamais stockées, toujours dérivées du registre (D321).
-- Effets sur les quantités d'un titre, après retrait des écritures corrigées et de leurs inverses.
-- Un transfert de titres sort du compte source et entre dans le compte de contrepartie.
CREATE VIEW core.position_ledger AS
WITH effective AS (
    SELECT t.*
    FROM core.transaction t
    WHERE t.reverses_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM core.transaction r WHERE r.owner_id = t.owner_id AND r.reverses_id = t.id)
)
SELECT owner_id, account_id, instrument_id, trade_date, created_at, id,
       CASE type WHEN 'split' THEN 'split' ELSE 'delta' END::text AS kind,
       CASE type WHEN 'sell' THEN -quantity WHEN 'split' THEN NULL ELSE quantity END AS delta,
       split_ratio
FROM effective
WHERE type IN ('buy', 'sell', 'drip', 'split') AND instrument_id IS NOT NULL
UNION ALL
SELECT owner_id, account_id, instrument_id, trade_date, created_at, id, 'delta'::text, -quantity, NULL::numeric
FROM effective
WHERE type = 'transfer' AND instrument_id IS NOT NULL AND quantity IS NOT NULL
UNION ALL
SELECT owner_id, counterpart_account_id, instrument_id, trade_date, created_at, id, 'delta'::text, quantity, NULL::numeric
FROM effective
WHERE type = 'transfer' AND instrument_id IS NOT NULL AND quantity IS NOT NULL;

-- Agrégat ordonné : un fractionnement multiplie la quantité détenue, un achat ou une vente l'ajuste.
-- +goose StatementBegin
CREATE FUNCTION core.fold_position_step(state numeric, kind text, delta numeric, ratio numeric)
RETURNS numeric LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN kind = 'split' THEN state * ratio ELSE state + delta END
$$;
-- +goose StatementEnd
CREATE AGGREGATE core.fold_position(kind text, delta numeric, ratio numeric) (
    SFUNC = core.fold_position_step,
    STYPE = numeric,
    INITCOND = '0'
);

-- Positions à ce jour, tous comptes.
CREATE VIEW core.position AS
SELECT owner_id, account_id, instrument_id,
       core.fold_position(kind, delta, split_ratio ORDER BY trade_date, created_at, id) AS quantity
FROM core.position_ledger
WHERE trade_date <= current_date
GROUP BY owner_id, account_id, instrument_id
HAVING core.fold_position(kind, delta, split_ratio ORDER BY trade_date, created_at, id) <> 0;

-- +goose Down
DROP VIEW core.position;
DROP AGGREGATE core.fold_position(text, numeric, numeric);
DROP FUNCTION core.fold_position_step(numeric, text, numeric, numeric);
DROP VIEW core.position_ledger;
DROP TABLE core.statement;
DROP TABLE core.transaction;
DROP FUNCTION core.transaction_check_reversal();
DROP FUNCTION core.transaction_immutable();
DROP TABLE core.account;
ALTER TABLE core.instrument DROP CONSTRAINT instrument_owner_id_id_key;
