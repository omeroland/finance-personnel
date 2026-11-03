// Package store contient les dépôts PostgreSQL du module d'évaluation d'actions.
//
// Règles : chaque requête filtre par owner_id ; toute écriture passe par une
// transaction et écrit exactement une ligne dans core.audit_log ; une écriture
// sans effet n'écrit rien. Ce package n'appelle jamais engine/.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store/storedb"
)

// Erreurs renvoyées par les dépôts.
var (
	// ErrNotFound : l'objet n'existe pas pour ce propriétaire.
	ErrNotFound = errors.New("introuvable")
	// ErrInvalid : entrée refusée à la frontière du dépôt.
	ErrInvalid = errors.New("entrée invalide")
)

type actorKey struct{}

// ContextWithActor porte l'identité (sub du JWT) inscrite dans core.audit_log.
// Sans acteur, le journal reçoit « system ».
func ContextWithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorFrom(ctx context.Context) string {
	if a, ok := ctx.Value(actorKey{}).(string); ok && a != "" {
		return a
	}
	return "system"
}

// Store implémente tous les dépôts du module au-dessus d'un pool pgx.
type Store struct {
	pool *pgxpool.Pool
}

// New construit le dépôt ; le pool doit se connecter avec le rôle finance_app.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// inTx exécute fn dans une transaction ; commit si fn réussit, sinon rollback.
func (s *Store) inTx(ctx context.Context, fn func(q *storedb.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("ouvrir la transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(storedb.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("valider la transaction: %w", err)
	}
	return nil
}

func (s *Store) read() *storedb.Queries { return storedb.New(s.pool) }

// audit écrit la ligne d'audit d'une écriture, dans la transaction courante.
func audit(ctx context.Context, q *storedb.Queries, owner uuid.UUID, action, entity string, before, after any) error {
	b, err := marshalOpt(before)
	if err != nil {
		return err
	}
	a, err := marshalOpt(after)
	if err != nil {
		return err
	}
	if err := q.InsertAudit(ctx, storedb.InsertAuditParams{
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

// requireInstrument vérifie que l'instrument appartient au propriétaire : la clé
// étrangère seule n'interdit pas de viser l'instrument d'un autre.
func requireInstrument(ctx context.Context, q *storedb.Queries, owner, id uuid.UUID) error {
	if _, err := q.GetInstrument(ctx, storedb.GetInstrumentParams{OwnerID: owner, ID: id}); err != nil {
		return notFound(err, "instrument "+id.String())
	}
	return nil
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return fmt.Errorf("charger %s: %w", what, err)
}
