package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store/storedb"
)

// canonicalJSON renvoie le JSON canonique : clés triées (encoding/json trie les
// map), sans espaces, nombres conservés tels quels (json.Number).
func canonicalJSON(raw []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("JSON invalide: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("JSON invalide: données après la valeur")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("sérialiser le JSON canonique: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// InputsHash est l'empreinte SHA-256 (hexadécimale) du JSON canonique des entrées.
func InputsHash(inputs []byte) (string, error) {
	c, err := canonicalJSON(inputs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}

func toEvaluation(r storedb.StockevalEvaluation) Evaluation {
	return Evaluation{
		ID: r.ID, InstrumentID: r.InstrumentID, Mode: r.Mode, RulesVersion: r.RulesVersion,
		RulesHash: r.RulesHash, InputsHash: r.InputsHash, Inputs: r.Inputs, Result: r.Result, CreatedAt: r.CreatedAt,
	}
}

// ArchiveEvaluation insère une évaluation (insertion seule) avec l'empreinte des entrées.
func (s *Store) ArchiveEvaluation(ctx context.Context, owner uuid.UUID, in EvaluationInput) (Evaluation, error) {
	if in.Mode == "" || in.RulesVersion == "" || in.RulesHash == "" {
		return Evaluation{}, fmt.Errorf("mode ou version des règles: %w", ErrInvalid)
	}
	inputs, err := canonicalJSON(in.Inputs)
	if err != nil {
		return Evaluation{}, fmt.Errorf("entrées: %w", ErrInvalid)
	}
	result, err := canonicalJSON(in.Result)
	if err != nil {
		return Evaluation{}, fmt.Errorf("résultat: %w", ErrInvalid)
	}
	sum := sha256.Sum256(inputs)
	var out Evaluation
	err = s.inTx(ctx, func(q *storedb.Queries) error {
		if err := requireInstrument(ctx, q, owner, in.InstrumentID); err != nil {
			return err
		}
		row, err := q.InsertEvaluation(ctx, storedb.InsertEvaluationParams{
			OwnerID: owner, InstrumentID: in.InstrumentID, Mode: in.Mode, RulesVersion: in.RulesVersion,
			RulesHash: in.RulesHash, InputsHash: hex.EncodeToString(sum[:]), Inputs: inputs, Result: result,
		})
		if err != nil {
			return fmt.Errorf("archiver l'évaluation: %w", err)
		}
		out = toEvaluation(row)
		return audit(ctx, q, owner, "evaluation.archive", "stockeval.evaluation", nil,
			map[string]any{"id": out.ID, "instrument_id": out.InstrumentID, "mode": out.Mode, "inputs_hash": out.InputsHash})
	})
	return out, err
}

// GetEvaluation renvoie une évaluation archivée du propriétaire.
func (s *Store) GetEvaluation(ctx context.Context, owner, id uuid.UUID) (Evaluation, error) {
	r, err := s.read().GetEvaluation(ctx, storedb.GetEvaluationParams{OwnerID: owner, ID: id})
	if err != nil {
		return Evaluation{}, notFound(err, "évaluation "+id.String())
	}
	return toEvaluation(r), nil
}

// ListEvaluations renvoie les évaluations d'un instrument, la plus récente d'abord.
func (s *Store) ListEvaluations(ctx context.Context, owner, instrumentID uuid.UUID, limit int) ([]Evaluation, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.read().ListEvaluations(ctx, storedb.ListEvaluationsParams{
		OwnerID: owner, InstrumentID: instrumentID, MaxRows: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("lister les évaluations de %s: %w", instrumentID, err)
	}
	out := make([]Evaluation, 0, len(rows))
	for _, r := range rows {
		out = append(out, toEvaluation(r))
	}
	return out, nil
}
