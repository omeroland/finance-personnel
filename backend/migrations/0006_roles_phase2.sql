-- +goose Up
-- Droits minimaux de finance_app pour la phase 2 (D323). Ni TRUNCATE, ni REFERENCES, ni TRIGGER.
GRANT USAGE ON SCHEMA etf TO finance_app;

-- Registre et versions de modèle : insertion seule.
GRANT SELECT, INSERT ON core.transaction, etf.model_version, etf.model_holding TO finance_app;

-- Comptes, relevés, profils (correction à la même date) et modèles : pas de DELETE.
GRANT SELECT, INSERT, UPDATE ON core.account, core.statement, etf.etf_profile, etf.model TO finance_app;

GRANT SELECT ON core.position_ledger, core.position TO finance_app;
GRANT EXECUTE ON FUNCTION core.fold_position_step(numeric, text, numeric, numeric) TO finance_app;

-- +goose Down
REVOKE ALL ON core.transaction, core.account, core.statement, core.position_ledger, core.position,
    etf.etf_profile, etf.model, etf.model_version, etf.model_holding FROM finance_app;
REVOKE EXECUTE ON FUNCTION core.fold_position_step(numeric, text, numeric, numeric) FROM finance_app;
REVOKE USAGE ON SCHEMA etf FROM finance_app;
