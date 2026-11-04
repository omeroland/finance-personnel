package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib" // pilote database/sql pour goose
	"github.com/pressly/goose/v3"

	"github.com/omeroland/finance-privee/backend/migrations"
)

// migrateConfig est la configuration de la sous-commande `migrate` (D221).
type migrateConfig struct {
	// ownerURL se connecte avec le propriétaire de la base (jamais finance_app).
	ownerURL string
	// appPassword est le mot de passe à donner au rôle finance_app (D41) ; il vient de
	// l'environnement, jamais d'une migration.
	appPassword string
}

// migrateConfigFromEnv lit MIGRATE_DATABASE_URL et FINANCE_APP_PASSWORD.
func migrateConfigFromEnv(getenv func(string) string) migrateConfig {
	return migrateConfig{ownerURL: getenv("MIGRATE_DATABASE_URL"), appPassword: getenv("FINANCE_APP_PASSWORD")}
}

// runMigrate applique les migrations goose embarquées (up), puis donne LOGIN et le
// mot de passe au rôle finance_app. Idempotent.
func runMigrate(ctx context.Context, logger *slog.Logger, cfg migrateConfig) error {
	if cfg.ownerURL == "" {
		return errors.New("MIGRATE_DATABASE_URL requis")
	}
	if cfg.appPassword == "" {
		return errors.New("FINANCE_APP_PASSWORD requis")
	}
	db, err := sql.Open("pgx", cfg.ownerURL)
	if err != nil {
		return fmt.Errorf("ouvrir la base: %w", err)
	}
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("initialiser goose: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("appliquer les migrations: %w", err)
	}
	logger.InfoContext(ctx, "migrations appliquées", slog.Int("nouvelles", len(results)))

	if err := setAppRolePassword(ctx, db, cfg.appPassword); err != nil {
		return err
	}
	logger.InfoContext(ctx, "rôle finance_app activé")
	return nil
}

// setAppRolePassword exécute ALTER ROLE finance_app LOGIN PASSWORD '…'. ALTER ROLE
// n'accepte pas de paramètre lié : le littéral est construit par le serveur avec
// format(%L), qui le protège contre toute injection.
func setAppRolePassword(ctx context.Context, db *sql.DB, password string) error {
	var stmt string
	if err := db.QueryRowContext(ctx,
		`SELECT format('ALTER ROLE finance_app LOGIN PASSWORD %L', $1::text)`, password).Scan(&stmt); err != nil {
		return fmt.Errorf("préparer ALTER ROLE: %w", err)
	}
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("donner LOGIN au rôle finance_app: %w", err)
	}
	return nil
}
