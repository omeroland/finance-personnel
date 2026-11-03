package httpapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// find résout un symbole canonique dans la liste de suivi du propriétaire.
// La liste d'un seul utilisateur est courte : un parcours suffit.
func (h *Handler) find(ctx context.Context, symbol string) (store.Instrument, error) {
	if err := checkSymbol(symbol); err != nil {
		return store.Instrument{}, err
	}
	list, err := h.repos.ListWatchlist(ctx, owner(ctx))
	if err != nil {
		return store.Instrument{}, fmt.Errorf("lister la liste de suivi: %w", err)
	}
	for _, i := range list {
		if i.Symbol == symbol {
			return i, nil
		}
	}
	return store.Instrument{}, httpx.NotFound("Aucune action " + symbol + " dans la liste de suivi.")
}

// load lit faits, instantané et jugement qualitatif ; un instantané ou un jugement absent reste nil.
func (h *Handler) load(ctx context.Context, inst store.Instrument) (stockData, error) {
	o := owner(ctx)
	d := stockData{inst: inst}
	var err error
	if d.facts, err = h.repos.ListFacts(ctx, o, inst.ID); err != nil {
		return d, fmt.Errorf("lire les faits de %s: %w", inst.Symbol, err)
	}
	if s, err := h.repos.GetSnapshot(ctx, o, inst.ID); err == nil {
		d.snap = &s
	} else if !errors.Is(err, store.ErrNotFound) {
		return d, fmt.Errorf("lire l'instantané de %s: %w", inst.Symbol, err)
	}
	if q, err := h.repos.GetQualitative(ctx, o, inst.ID); err == nil {
		d.qual = &q
	} else if !errors.Is(err, store.ErrNotFound) {
		return d, fmt.Errorf("lire le jugement de %s: %w", inst.Symbol, err)
	}
	return d, nil
}

// ListStocks : liste de suivi avec score. Une action que le contrat ne sait pas
// représenter, ou dont le mode compat lève une erreur de la feuille, est omise
// de la liste (D202).
func (h *Handler) ListStocks(ctx context.Context, req gen.ListStocksRequestObject) (gen.ListStocksResponseObject, error) {
	mode, horizon, err := h.modeAndHorizon(req.Params.Mode, req.Params.Horizon)
	if err != nil {
		return nil, err
	}
	list, err := h.repos.ListWatchlist(ctx, owner(ctx))
	if err != nil {
		return nil, fmt.Errorf("lister la liste de suivi: %w", err)
	}
	out := gen.StockList{Items: []gen.StockSummary{}}
	for _, inst := range list {
		stock, ok := stockDTO(inst)
		if !ok {
			h.log.WarnContext(ctx, "action non représentable dans le contrat, omise", "symbol", inst.Symbol)
			continue
		}
		d, err := h.load(ctx, inst)
		if err != nil {
			return nil, err
		}
		ev, _, err := h.evaluate(d, mode)
		var he *httpx.Error
		if errors.As(err, &he) && he.Kind == "sheet-error" {
			h.log.InfoContext(ctx, "erreur de la feuille en compat, action omise", "symbol", inst.Symbol)
			continue
		}
		if err != nil {
			return nil, err
		}
		score, ok := summaryDTO(mode, ev, horizon)
		if !ok {
			continue
		}
		out.Items = append(out.Items, gen.StockSummary{Stock: stock, Score: score})
	}
	slices.SortFunc(out.Items, func(a, b gen.StockSummary) int { return strings.Compare(a.Stock.Symbol, b.Stock.Symbol) })
	return gen.ListStocks200JSONResponse(out), nil
}

// AddStock : ajoute une action à la liste de suivi.
func (h *Handler) AddStock(ctx context.Context, req gen.AddStockRequestObject) (gen.AddStockResponseObject, error) {
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	b := *req.Body
	var fe fieldErrors
	if !validSymbol(b.Symbol) {
		fe.add("symbol", "Le symbole doit suivre le format BOURSE:SYMBOLE.")
	}
	ex, exOK := exchangeOf(b.Symbol)
	if validSymbol(b.Symbol) && !exOK {
		fe.add("symbol", "La bourse doit être TSX, NYSE ou NASDAQ.")
	}
	checkName(&fe, b.Name)
	checkCurrency(&fe, "quoteCurrency", b.QuoteCurrency)
	checkCurrency(&fe, "reportingCurrency", b.ReportingCurrency)
	if err := fe.err(); err != nil {
		return nil, err
	}
	_, err := h.find(ctx, b.Symbol)
	if err == nil {
		return nil, httpx.Conflict("L'action " + b.Symbol + " est déjà dans la liste de suivi.")
	}
	var he *httpx.Error
	if !errors.As(err, &he) || he.Status != 404 {
		return nil, err
	}
	inst, err := h.repos.AddToWatchlist(ctx, owner(ctx), store.InstrumentInput{
		Symbol: b.Symbol, Name: strings.TrimSpace(b.Name), Exchange: string(ex),
		QuoteCurrency: string(b.QuoteCurrency), ReportCurrency: string(b.ReportingCurrency),
		IsFinancial: b.IsFinancial != nil && *b.IsFinancial,
	})
	if err != nil {
		return nil, fmt.Errorf("ajouter %s: %w", b.Symbol, err)
	}
	stock, ok := stockDTO(inst)
	if !ok {
		return nil, fmt.Errorf("l'instrument %s existant n'est pas représentable", b.Symbol)
	}
	return gen.AddStock201JSONResponse(stock), nil
}

// detail assemble la fiche complète.
func (h *Handler) detail(ctx context.Context, inst store.Instrument) (gen.StockDetail, error) {
	stock, ok := stockDTO(inst)
	if !ok {
		return gen.StockDetail{}, fmt.Errorf("l'instrument %s n'est pas représentable", inst.Symbol)
	}
	d, err := h.load(ctx, inst)
	if err != nil {
		return gen.StockDetail{}, err
	}
	conflicts, err := h.repos.ListOpenConflicts(ctx, owner(ctx), inst.ID)
	if err != nil {
		return gen.StockDetail{}, fmt.Errorf("lire les conflits de %s: %w", inst.Symbol, err)
	}
	out := gen.StockDetail{
		Stock: stock, Facts: factsDTO(d.facts, inst), Qualitative: qualitativeDTO(d.qual),
		Sources: sourcesDTO(d.facts), Conflicts: conflictsDTO(conflicts),
	}
	if d.snap != nil {
		out.Snapshot = ptr(snapshotDTO(*d.snap, inst))
	}
	return out, nil
}

// GetStock : fiche complète.
func (h *Handler) GetStock(ctx context.Context, req gen.GetStockRequestObject) (gen.GetStockResponseObject, error) {
	inst, err := h.find(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, inst)
	if err != nil {
		return nil, err
	}
	return gen.GetStock200JSONResponse(d), nil
}

// UpdateStock : saisie manuelle des métadonnées et de l'instantané.
func (h *Handler) UpdateStock(ctx context.Context, req gen.UpdateStockRequestObject) (gen.UpdateStockResponseObject, error) {
	inst, err := h.find(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	b := *req.Body
	var fe fieldErrors
	if b.Name == nil && b.QuoteCurrency == nil && b.ReportingCurrency == nil && b.IsFinancial == nil && b.Snapshot == nil {
		fe.add("body", "Au moins une propriété est requise.")
	}
	upd := store.InstrumentUpdate{
		Name: inst.Name, QuoteCurrency: inst.QuoteCurrency, ReportCurrency: inst.ReportCurrency, IsFinancial: inst.IsFinancial,
	}
	if b.Name != nil {
		checkName(&fe, *b.Name)
		upd.Name = strings.TrimSpace(*b.Name)
	}
	if b.QuoteCurrency != nil {
		checkCurrency(&fe, "quoteCurrency", *b.QuoteCurrency)
		upd.QuoteCurrency = string(*b.QuoteCurrency)
	}
	if b.ReportingCurrency != nil {
		checkCurrency(&fe, "reportingCurrency", *b.ReportingCurrency)
		upd.ReportCurrency = string(*b.ReportingCurrency)
	}
	if b.IsFinancial != nil {
		upd.IsFinancial = *b.IsFinancial
	}
	var snap *store.Snapshot
	if b.Snapshot != nil {
		cur, err := h.repos.GetSnapshot(ctx, owner(ctx), inst.ID)
		var curp *store.Snapshot
		switch {
		case err == nil:
			curp = &cur
		case !errors.Is(err, store.ErrNotFound):
			return nil, fmt.Errorf("lire l'instantané de %s: %w", inst.Symbol, err)
		}
		snap = mergeSnapshot(&fe, curp, *b.Snapshot, inst.ID, upd.QuoteCurrency, h.now())
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	if upd != (store.InstrumentUpdate{Name: inst.Name, QuoteCurrency: inst.QuoteCurrency, ReportCurrency: inst.ReportCurrency, IsFinancial: inst.IsFinancial}) {
		if inst, err = h.repos.UpdateInstrument(ctx, owner(ctx), inst.ID, upd); err != nil {
			return nil, fmt.Errorf("modifier %s: %w", inst.Symbol, err)
		}
	}
	if snap != nil {
		if _, err := h.repos.SaveSnapshot(ctx, owner(ctx), *snap); err != nil {
			return nil, fmt.Errorf("enregistrer l'instantané de %s: %w", inst.Symbol, err)
		}
	}
	d, err := h.detail(ctx, inst)
	if err != nil {
		return nil, err
	}
	return gen.UpdateStock200JSONResponse(d), nil
}

// DeleteStock : retire l'action de la liste (l'archive reste).
func (h *Handler) DeleteStock(ctx context.Context, req gen.DeleteStockRequestObject) (gen.DeleteStockResponseObject, error) {
	inst, err := h.find(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	if err := h.repos.RemoveFromWatchlist(ctx, owner(ctx), inst.ID); err != nil {
		return nil, fmt.Errorf("retirer %s: %w", inst.Symbol, err)
	}
	return gen.DeleteStock204Response{}, nil
}

// ReplaceFacts : saisie manuelle des valeurs annuelles. Une valeur manuelle remplace toute source (D100).
func (h *Handler) ReplaceFacts(ctx context.Context, req gen.ReplaceFactsRequestObject) (gen.ReplaceFactsResponseObject, error) {
	inst, err := h.find(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	var fe fieldErrors
	if n := len(req.Body.Facts); n < 1 || n > 400 {
		fe.add("facts", "De 1 à 400 valeurs sont attendues.")
	}
	type key struct {
		year   int
		metric gen.Metric
	}
	seen := map[key]bool{}
	writes := make([]store.Fact, 0, len(req.Body.Facts))
	for i, f := range req.Body.Facts {
		field := fmt.Sprintf("facts[%d]", i)
		sm, ok := metricToStore[f.Metric]
		if !ok {
			fe.add(field+".metric", "Mesure inconnue.")
		}
		if f.FiscalYear < 1980 || f.FiscalYear > 2100 {
			fe.add(field+".fiscalYear", "L'année fiscale doit être comprise entre 1980 et 2100.")
		}
		checkCurrency(&fe, field+".currency", f.Currency)
		v, vok := parseDecimal(&fe, field+".value", f.Value)
		if k := (key{f.FiscalYear, f.Metric}); seen[k] {
			fe.add(field, "Couple (année, mesure) en double.")
		} else {
			seen[k] = true
		}
		if !ok || !vok {
			continue
		}
		w := store.Fact{InstrumentID: inst.ID, FiscalYear: f.FiscalYear, Metric: sm, Value: v, Source: store.SourceManual}
		// Le rendement et le nombre d'actions n'ont pas de devise (D130).
		if f.Metric == gen.DilutedEps || f.Metric == gen.DividendPerShare {
			w.Currency = ptr(string(f.Currency))
		}
		writes = append(writes, w)
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	for _, w := range writes {
		if _, err := h.repos.WriteFact(ctx, owner(ctx), w); err != nil {
			return nil, fmt.Errorf("écrire le fait %s/%d de %s: %w", w.Metric, w.FiscalYear, inst.Symbol, err)
		}
		// La valeur manuelle tranche : les conflits ouverts de ce couple sont résolus (D209).
		if _, err := h.repos.ResolveConflicts(ctx, owner(ctx), inst.ID, w.FiscalYear, w.Metric); err != nil {
			return nil, fmt.Errorf("résoudre les conflits %s/%d de %s: %w", w.Metric, w.FiscalYear, inst.Symbol, err)
		}
	}
	facts, err := h.repos.ListFacts(ctx, owner(ctx), inst.ID)
	if err != nil {
		return nil, fmt.Errorf("lire les faits de %s: %w", inst.Symbol, err)
	}
	return gen.ReplaceFacts200JSONResponse(gen.FactList{Facts: factsDTO(facts, inst)}), nil
}

// ReplaceQualitative : jugement qualitatif (règles 1 à 4, rachat manuel).
func (h *Handler) ReplaceQualitative(ctx context.Context, req gen.ReplaceQualitativeRequestObject) (gen.ReplaceQualitativeResponseObject, error) {
	inst, err := h.find(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	var fe fieldErrors
	for name, r := range map[string]gen.RuleAnswer{"rule1": req.Body.Rule1, "rule2": req.Body.Rule2, "rule3": req.Body.Rule3, "rule4": req.Body.Rule4} {
		if !r.Answer.Valid() {
			fe.add(name+".answer", "La réponse doit être yes, no ou unanswered.")
		}
		if r.Note != nil && len([]rune(*r.Note)) > 2000 {
			fe.add(name+".note", "La note est limitée à 2000 caractères.")
		}
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	q, err := qualitativeToStore(*req.Body, inst.ID, h.now())
	if err != nil {
		return nil, err
	}
	if err := h.repos.SaveQualitative(ctx, owner(ctx), q); err != nil {
		return nil, fmt.Errorf("enregistrer le jugement de %s: %w", inst.Symbol, err)
	}
	saved, err := h.repos.GetQualitative(ctx, owner(ctx), inst.ID)
	if err != nil {
		return nil, fmt.Errorf("relire le jugement de %s: %w", inst.Symbol, err)
	}
	return gen.ReplaceQualitative200JSONResponse(qualitativeDTO(&saved)), nil
}
