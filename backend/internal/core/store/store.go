// Package store contient les dépôts PostgreSQL du socle : comptes, registre de
// transactions, relevés et positions dérivées. Chaque requête filtre par
// owner_id ; toute écriture passe par une transaction et écrit dans core.audit_log.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
)

// Erreurs renvoyées par les dépôts.
var (
	// ErrNotFound : l'objet n'existe pas pour ce propriétaire.
	ErrNotFound = errors.New("introuvable")
	// ErrConflict : la ressource existe déjà (clé unique).
	ErrConflict = errors.New("conflit")
	// ErrInvalid : entrée refusée par le dépôt.
	ErrInvalid = errors.New("entrée invalide")
)

// InvalidError est un refus de validation dont le message est sûr à montrer.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

// Is fait de InvalidError une ErrInvalid.
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

// NotFoundError est une absence dont le message est sûr à montrer.
type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }

// Is fait de NotFoundError une ErrNotFound.
func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

type actorKey struct{}

// ContextWithActor porte l'identité (sub du JWT) inscrite dans core.audit_log.
func ContextWithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorFrom(ctx context.Context) string {
	if a, ok := ctx.Value(actorKey{}).(string); ok && a != "" {
		return a
	}
	return "system"
}

// Store implémente les dépôts du socle au-dessus d'un pool pgx.
type Store struct{ pool *pgxpool.Pool }

// New construit le dépôt ; le pool se connecte avec le rôle finance_app.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool expose le pool pour les dépôts des modules qui partagent la connexion.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// InTx exécute fn dans une transaction ; commit si fn réussit, sinon rollback.
func (s *Store) InTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("ouvrir la transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPg(fmt.Errorf("valider la transaction: %w", err))
	}
	return nil
}

func (s *Store) read() *coredb.Queries { return coredb.New(s.pool) }

// Audit écrit la ligne d'audit d'une écriture, dans la transaction courante.
// Les données passées ne doivent contenir aucun montant de portefeuille sensible
// au-delà de ce que le registre porte déjà : la table est protégée par propriétaire.
func Audit(ctx context.Context, q *coredb.Queries, owner uuid.UUID, action, entity string, before, after any) error {
	b, err := marshalOpt(before)
	if err != nil {
		return err
	}
	a, err := marshalOpt(after)
	if err != nil {
		return err
	}
	if err := q.InsertAudit(ctx, coredb.InsertAuditParams{
		OwnerID: owner, Actor: actorFrom(ctx), Action: action, Entity: entity, Before: b, After: a,
	}); err != nil {
		return fmt.Errorf("écrire l'audit %s: %w", action, err)
	}
	return nil
}

func marshalOpt(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("sérialiser l'audit: %w", err)
	}
	return b, nil
}

// MapPg traduit les erreurs de contrainte PostgreSQL en erreurs du dépôt, sans
// jamais exposer le message de la base.
func MapPg(err error) error { return mapPg(err) }

func mapPg(err error) error {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case "23505":
		return fmt.Errorf("%w", ErrConflict)
	case "23514", "23503", "23502", "22003", "22P02", "23000":
		return &InvalidError{Message: "Une valeur est refusée par les règles du registre."}
	}
	return err
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return &NotFoundError{Message: what + " introuvable."}
	}
	return fmt.Errorf("charger %s: %w", what, err)
}
