package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

type heldPosition struct {
	symbol string
	qty    decimal.Decimal
}

func accountIDs(p *gen.Accounts) []uuid.UUID {
	if p == nil {
		return nil
	}
	return *p
}

// GetExposure : exposition par transparence des comptes demandés, comparée au modèle s'il est fourni.
// La répartition de chaque fonds est complétée (D340) avant le moteur ; les positions sans prix ou
// sans profil sont écartées et comptées dans le journal (D412).
func (h *Handler) GetExposure(ctx context.Context, req gen.GetExposureRequestObject) (gen.GetExposureResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := h.resolveAccounts(ctx, owner, accountIDs(req.Params.Accounts))
	if err != nil {
		return nil, err
	}
	cur := gen.CAD
	for i, a := range accounts {
		c := gen.Currency(strings.TrimSpace(a.Currency))
		if i == 0 {
			cur = c
		} else if c != cur {
			return nil, httpx.BadRequest("Les comptes demandés n'ont pas tous la même devise (pas de conversion en phase 2).")
		}
	}
	var model *gen.Model
	var mdl struct {
		targets engine.Allocation
	}
	if req.Params.ModelId != nil {
		m, err := h.pf.GetModel(ctx, owner, *req.Params.ModelId)
		if err != nil {
			return nil, translate(fmt.Errorf("lire le modèle: %w", err))
		}
		dto := modelDTO(m)
		model = &dto
		mdl.targets = engineAlloc(m.Targets)
	}

	today := h.today()
	symbolSet := map[string]bool{}
	var all []heldPosition
	for _, a := range accounts {
		hold, err := h.ledger.Holdings(ctx, owner, a.ID, today)
		if err != nil {
			return nil, translate(fmt.Errorf("lire les positions: %w", err))
		}
		for _, p := range hold.Positions {
			symbolSet[p.Symbol] = true
			all = append(all, heldPosition{symbol: p.Symbol, qty: p.Quantity})
		}
	}
	symbols := make([]string, 0, len(symbolSet))
	for s := range symbolSet {
		symbols = append(symbols, s)
	}
	prices, err := h.pf.LastPrices(ctx, owner, symbols, string(cur), today)
	if err != nil {
		return nil, translate(fmt.Errorf("lire les prix: %w", err))
	}
	profiles, err := h.profiles(ctx, owner)
	if err != nil {
		return nil, err
	}
	var holdings []engine.Holding
	skipped := 0
	for _, p := range all {
		price, okPrice := prices[p.symbol]
		prof, okProf := profiles[p.symbol]
		if !okPrice || !okProf {
			skipped++
			continue
		}
		alloc, err := fullAllocation(prof)
		if err != nil {
			skipped++
			continue
		}
		holdings = append(holdings, engine.Holding{Symbol: p.symbol, Quantity: p.qty, Price: price, Allocation: alloc, MER: prof.Mer})
	}
	if skipped > 0 {
		h.log.WarnContext(ctx, "positions écartées de l'exposition (prix ou profil manquant)", "count", skipped)
	}
	res, err := engine.ExposureOfHoldings(holdings)
	if err != nil {
		return nil, planError(fmt.Errorf("exposition: %w", err))
	}
	out := gen.Exposure{AsOf: dateOf(today), Currency: cur, Classes: []gen.ClassExposure{}}
	if model != nil {
		id := model.Id
		out.ModelId = &id
	}
	if res.Empty {
		return gen.GetExposure200JSONResponse(out), nil
	}
	total := dec(res.Value)
	out.TotalValue = &total
	if res.HasMER {
		mer := res.MER
		out.WeightedMer = &mer
	}
	var dev engine.Allocation
	if model != nil {
		dev = engine.Deviation(res.Exposure, mdl.targets)
	}
	for _, c := range allClasses {
		ce := gen.ClassExposure{AssetClass: gen.AssetClass(c), Actual: res.Exposure.Get(c)}
		if model != nil {
			t, d := mdl.targets.Get(c), dev.Get(c)
			ce.Target, ce.Deviation = &t, &d
		}
		out.Classes = append(out.Classes, ce)
	}
	return gen.GetExposure200JSONResponse(out), nil
}
