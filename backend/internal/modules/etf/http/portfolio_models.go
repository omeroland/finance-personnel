package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Les six classes d'actifs, dans l'ordre canonique du moteur.
var allClasses = engine.Classes

func engineAlloc(a ingest.Allocation) engine.Allocation {
	return engine.Allocation{
		EquityCA: a.EquityCA, EquityUS: a.EquityUS, EquityIntl: a.EquityIntl,
		BondCA: a.BondCA, BondOther: a.BondOther, OtherAssets: a.OtherAssets,
	}
}

func ingestAlloc(a engine.Allocation) ingest.Allocation {
	return ingest.Allocation{
		EquityCA: a.EquityCA, EquityUS: a.EquityUS, EquityIntl: a.EquityIntl,
		BondCA: a.BondCA, BondOther: a.BondOther, OtherAssets: a.OtherAssets,
	}
}

func classTargets(a engine.Allocation) []gen.ClassTarget {
	out := make([]gen.ClassTarget, 0, len(allClasses))
	for _, c := range allClasses {
		out = append(out, gen.ClassTarget{AssetClass: gen.AssetClass(c), Target: a.Get(c)})
	}
	return out
}

func modelDTO(m etfstore.Model) gen.Model {
	cash, rel, cp := m.CashTarget, m.BandRelative, m.BandCash
	out := gen.Model{
		Id: m.ID, Name: m.Name, Version: m.Version, CreatedAt: m.CreatedAt,
		Targets: classTargets(engineAlloc(m.Targets)), CashTarget: &cash,
		Bands:    &gen.Bands{Relative: &rel, CashPoints: &cp},
		Holdings: make([]gen.ModelHolding, 0, len(m.Holdings)),
	}
	for _, h := range m.Holdings {
		out.Holdings = append(out.Holdings, gen.ModelHolding{Symbol: h.Symbol, Weight: h.Weight})
	}
	return out
}

// ListModels : dernière version de chaque modèle du propriétaire.
func (h *Handler) ListModels(ctx context.Context, _ gen.ListModelsRequestObject) (gen.ListModelsResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.pf.ListModels(ctx, owner)
	if err != nil {
		return nil, translate(fmt.Errorf("lister les modèles: %w", err))
	}
	out := gen.ModelList{Items: make([]gen.Model, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, modelDTO(r))
	}
	return gen.ListModels200JSONResponse(out), nil
}

// CreateModel : crée un modèle à la version 1 (409 si le nom est pris).
func (h *Handler) CreateModel(ctx context.Context, req gen.CreateModelRequestObject) (gen.CreateModelResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	in, err := h.modelInput(ctx, owner, req.Body)
	if err != nil {
		return nil, err
	}
	m, err := h.pf.CreateModel(ctx, owner, in)
	if err != nil {
		return nil, translate(fmt.Errorf("créer le modèle: %w", err))
	}
	return gen.CreateModel201JSONResponse(modelDTO(m)), nil
}

// UpdateModel : publie la version suivante d'un modèle (404 s'il n'existe pas).
func (h *Handler) UpdateModel(ctx context.Context, req gen.UpdateModelRequestObject) (gen.UpdateModelResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	in, err := h.modelInput(ctx, owner, req.Body)
	if err != nil {
		return nil, err
	}
	m, err := h.pf.PublishModelVersion(ctx, owner, req.Id, in)
	if err != nil {
		return nil, translate(fmt.Errorf("publier la version: %w", err))
	}
	return gen.UpdateModel200JSONResponse(modelDTO(m)), nil
}

// modelInput valide le corps d'un modèle à la frontière (D313) et le traduit.
// Les totaux sont vérifiés par le moteur, jamais corrigés en silence.
func (h *Handler) modelInput(ctx context.Context, owner uuid.UUID, body *gen.ModelWrite) (ingest.Model, error) {
	if body == nil {
		return ingest.Model{}, httpx.BadRequest("Corps de requête requis.")
	}
	b := *body
	var fe fieldErrors
	fe.text("name", b.Name, 1, 100)
	in := ingest.Model{Name: b.Name, BandRelative: engine.DefaultRelativeBand, BandCash: engine.DefaultCashPoints}
	if b.CashTarget != nil {
		in.CashTarget = *b.CashTarget
	}
	if b.Bands != nil {
		if v := b.Bands.Relative; v != nil {
			in.BandRelative = *v
		}
		if v := b.Bands.CashPoints; v != nil {
			in.BandCash = *v
		}
	}
	if !(in.BandRelative >= 0 && in.BandRelative <= 1) || !(in.BandCash >= 0 && in.BandCash <= 1) {
		fe.add("bands", "Les bandes sont des fractions entre 0 et 1.")
	}
	if len(b.Targets) > len(allClasses) {
		fe.add("targets", "Une cible par classe d'actifs au plus.")
	}
	var alloc engine.Allocation
	seen := map[gen.AssetClass]bool{}
	for _, t := range b.Targets {
		if !t.AssetClass.Valid() || seen[t.AssetClass] {
			fe.add("targets", "Classe d'actifs inconnue ou en double.")
			continue
		}
		seen[t.AssetClass] = true
		setClass(&alloc, engine.Class(t.AssetClass), t.Target)
	}
	if len(b.Holdings) > etfstore.MaxModelHoldings {
		fe.add("holdings", "Un modèle compte 20 fonds au plus.")
	}
	inCatalog, err := h.catalogSymbols(ctx, owner)
	if err != nil {
		return ingest.Model{}, err
	}
	weights := make([]float64, 0, len(b.Holdings))
	syms := map[string]bool{}
	for _, hd := range b.Holdings {
		switch {
		case !symbolRe.MatchString(hd.Symbol) || syms[hd.Symbol]:
			fe.add("holdings", "Symbole invalide ou en double : "+hd.Symbol+".")
		case !inCatalog[hd.Symbol]:
			fe.add("holdings", "Le fonds "+hd.Symbol+" n'est pas au catalogue.")
		}
		syms[hd.Symbol] = true
		weights = append(weights, hd.Weight)
		in.Holdings = append(in.Holdings, ingest.ModelHolding{Symbol: hd.Symbol, Weight: hd.Weight})
	}
	if len(fe) == 0 {
		var te *engine.TargetsError
		if err := engine.CheckTargets(alloc, in.CashTarget); errors.As(err, &te) {
			fe.add("targets", "Les cibles par classe et la cible d'encaisse doivent totaliser 100 %.")
		}
		if err := engine.CheckWeights(weights, in.CashTarget); errors.As(err, &te) {
			fe.add("holdings", "Les poids des fonds et la cible d'encaisse doivent totaliser 100 %.")
		}
	}
	if err := fe.err(); err != nil {
		return ingest.Model{}, err
	}
	in.Targets = ingestAlloc(alloc)
	return in, nil
}

func setClass(a *engine.Allocation, c engine.Class, v float64) {
	switch c {
	case engine.EquityCA:
		a.EquityCA = v
	case engine.EquityUS:
		a.EquityUS = v
	case engine.EquityIntl:
		a.EquityIntl = v
	case engine.BondCA:
		a.BondCA = v
	case engine.BondOther:
		a.BondOther = v
	case engine.OtherAssets:
		a.OtherAssets = v
	}
}

func (h *Handler) catalogSymbols(ctx context.Context, owner uuid.UUID) (map[string]bool, error) {
	rows, err := h.catalog.ListCatalog(ctx, owner, etfstore.CatalogFilter{})
	if err != nil {
		return nil, translate(fmt.Errorf("lister le catalogue: %w", err))
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.Symbol] = true
	}
	return out, nil
}
