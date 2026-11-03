-- +goose Up
-- Rôle applicatif sans connexion par défaut : le déploiement lui donne LOGIN et un
-- mot de passe hors dépôt (D41).
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'finance_app') THEN
        CREATE ROLE finance_app NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA core, stockeval TO finance_app;

-- Insertion seule sur les archives.
GRANT SELECT, INSERT ON core.audit_log, stockeval.evaluation TO finance_app;

GRANT SELECT, INSERT, UPDATE, DELETE ON
    core.instrument,
    stockeval.watchlist,
    stockeval.annual_fact,
    stockeval.fact_conflict,
    stockeval.snapshot,
    stockeval.qualitative,
    stockeval.import_job
TO finance_app;

-- +goose Down
-- Les droits disparaissent avec les schémas ; le rôle, global au serveur, est conservé.
REVOKE ALL ON ALL TABLES IN SCHEMA core, stockeval FROM finance_app;
REVOKE USAGE ON SCHEMA core, stockeval FROM finance_app;
