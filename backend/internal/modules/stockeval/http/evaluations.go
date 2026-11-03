package httpapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// computed calcule l'évaluation courante d'une action (sans l'archiver).
func (h *Handler) computed(ctx context.Context, symbol string, mode gen.Mode) (store.Instrument, gen.Evaluation, archivedInputs, error) {
	inst, err := h.find(ctx, symbol)
	if err != nil {
		return inst, gen.Evaluation{}, archivedInputs{}, err
	}
	d, err := h.load(ctx, inst)
	if err != nil {
		return inst, gen.Evaluation{}, archivedInputs{}, err
	}
	conflicts, err := h.repos.ListOpenConflicts(ctx, owner(ctx), inst.ID)
	if err != nil {
		return inst, gen.Evaluation{}, archivedInputs{}, fmt.Errorf("lire les conflits de %s: %w", symbol, err)
	}
	ev, arch, err := h.evaluate(d, mode)
	if err != nil {
		return inst, gen.Evaluation{}, arch, err
	}
	return inst, evaluationDTO(symbol, mode, ev, h.now(), conflictWarnings(conflicts)), arch, nil
}

// GetEvaluation : évaluation à la volée, non archivée.
func (h *Handler) GetEvaluation(ctx context.Context, req gen.GetEvaluationRequestObject) (gen.GetEvaluationResponseObject, error) {
	mode, err := parseMode(req.Params.Mode)
	if err != nil {
		return nil, err
	}
	_, ev, _, err := h.computed(ctx, req.Symbol, mode)
	if err != nil {
		return nil, err
	}
	return gen.GetEvaluation200JSONResponse(ev), nil
}

// CreateEvaluation : calcule puis archive (insertion seule).
func (h *Handler) CreateEvaluation(ctx context.Context, req gen.CreateEvaluationRequestObject) (gen.CreateEvaluationResponseObject, error) {
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	mode, err := parseMode(req.Body.Mode)
	if err != nil {
		return nil, err
	}
	inst, ev, arch, err := h.computed(ctx, req.Symbol, mode)
	if err != nil {
		return nil, err
	}
	inputs, err := json.Marshal(arch)
	if err != nil {
		return nil, fmt.Errorf("sérialiser les entrées: %w", err)
	}
	result, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("sérialiser le résultat: %w", err)
	}
	row, err := h.repos.ArchiveEvaluation(ctx, owner(ctx), store.EvaluationInput{
		InstrumentID: inst.ID, Mode: string(mode), RulesVersion: h.rules.Version, RulesHash: h.rules.Hash,
		Inputs: inputs, Result: result,
	})
	if err != nil {
		return nil, fmt.Errorf("archiver l'évaluation de %s: %w", req.Symbol, err)
	}
	rec, err := recordDTO(row)
	if err != nil {
		return nil, err
	}
	return gen.CreateEvaluation201JSONResponse(rec), nil
}

func recordDTO(r store.Evaluation) (gen.EvaluationRecord, error) {
	var ev gen.Evaluation
	if err := json.Unmarshal(r.Result, &ev); err != nil {
		return gen.EvaluationRecord{}, fmt.Errorf("lire l'évaluation archivée %s: %w", r.ID, err)
	}
	return gen.EvaluationRecord{
		Id: r.ID, CreatedAt: r.CreatedAt.UTC(), Mode: gen.Mode(r.Mode), RulesVersion: r.RulesVersion,
		InputsFingerprint: r.InputsHash, Evaluation: ev,
	}, nil
}

// maxHistory borne l'historique renvoyé (le contrat n'a pas de pagination, D39).
const maxHistory = 500

// ListEvaluations : historique archivé, du plus récent au plus ancien.
func (h *Handler) ListEvaluations(ctx context.Context, req gen.ListEvaluationsRequestObject) (gen.ListEvaluationsResponseObject, error) {
	inst, err := h.find(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	rows, err := h.repos.ListEvaluations(ctx, owner(ctx), inst.ID, maxHistory)
	if err != nil {
		return nil, fmt.Errorf("lister les évaluations de %s: %w", req.Symbol, err)
	}
	out := gen.EvaluationRecordList{Items: make([]gen.EvaluationRecord, 0, len(rows))}
	for _, r := range rows {
		rec, err := recordDTO(r)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, rec)
	}
	return gen.ListEvaluations200JSONResponse(out), nil
}
