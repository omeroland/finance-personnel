package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// ListPositions : positions et encaisse dérivées du registre. Aucun prix de
// marché n'est stocké en phase 2 (D402) : prix et valeur de marché sont absents.
func (h *Handler) ListPositions(ctx context.Context, req gen.ListPositionsRequestObject) (gen.ListPositionsResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	asOf := h.now().UTC().Truncate(24 * time.Hour)
	if req.Params.AsOf != nil {
		asOf = req.Params.AsOf.Time
		if !validDate(asOf) {
			return nil, httpx.BadRequest(invalidFields, httpx.FieldError{Field: "asOf", Message: "La date doit être comprise entre 1900 et 2100."})
		}
	}
	hold, err := h.ledger.Holdings(ctx, owner, req.Id, asOf)
	if err != nil {
		return nil, translate(fmt.Errorf("lire les positions: %w", err))
	}
	cur := gen.Currency(strings.TrimSpace(hold.Account.Currency))
	out := gen.PositionList{
		AccountId: hold.Account.ID, AsOf: dateOf(asOf), Cash: dec(hold.Cash), Currency: cur,
		Positions: make([]gen.Position, 0, len(hold.Positions)),
	}
	for _, p := range hold.Positions {
		name := p.Name
		dto := gen.Position{Symbol: p.Symbol, Name: &name, Quantity: dec(p.Quantity), Currency: cur}
		if p.AverageCost != nil {
			c := dec(*p.AverageCost)
			dto.AverageCost = &c
		}
		out.Positions = append(out.Positions, dto)
	}
	if len(hold.Positions) == 0 {
		t := dec(hold.Cash)
		out.TotalValue = &t
	}
	return gen.ListPositions200JSONResponse(out), nil
}

// ListEtfs : catalogue filtrable (type, région, RFG maximal).
func (h *Handler) ListEtfs(ctx context.Context, req gen.ListEtfsRequestObject) (gen.ListEtfsResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	var fe fieldErrors
	f := etfstore.CatalogFilter{}
	if p := req.Params.Type; p != nil {
		if !p.Valid() {
			fe.add("type", "Type de FNB inconnu.")
		}
		f.Type = string(*p)
	}
	if p := req.Params.Region; p != nil {
		if !p.Valid() {
			fe.add("region", "Région inconnue.")
		}
		f.Region = string(*p)
	}
	if p := req.Params.MaxMer; p != nil {
		if *p < 0 || *p > 1 || *p != *p {
			fe.add("maxMer", "Le RFG maximal est une fraction entre 0 et 1.")
		}
		f.MaxMER = p
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	rows, err := h.catalog.ListCatalog(ctx, owner, f)
	if err != nil {
		return nil, translate(fmt.Errorf("lister le catalogue: %w", err))
	}
	out := gen.EtfList{Items: make([]gen.Etf, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, etfDTO(r))
	}
	return gen.ListEtfs200JSONResponse(out), nil
}

func etfDTO(r etfstore.Catalog) gen.Etf {
	alloc := []gen.AssetAllocation{}
	for _, a := range []struct {
		c gen.AssetClass
		w float64
	}{
		{gen.EquityCa, r.EquityCa},
		{gen.EquityUs, r.EquityUs},
		{gen.EquityIntl, r.EquityIntl},
		{gen.BondCa, r.BondCa},
		{gen.BondOther, r.BondOther},
		{gen.OtherAssets, r.OtherAssets},
	} {
		if a.w > 0 {
			alloc = append(alloc, gen.AssetAllocation{AssetClass: a.c, Weight: a.w})
		}
	}
	out := gen.Etf{
		Symbol: r.Symbol, Name: r.Name, Type: gen.EtfType(r.Type), Currency: gen.Currency(strings.TrimSpace(r.QuoteCurrency)),
		Allocation: alloc, Mer: r.Mer, Esg: r.Esg, AsOf: dateOf(r.AsOf),
	}
	if r.Region != nil {
		reg := gen.EtfRegion(*r.Region)
		out.Region = &reg
	}
	if r.Size.Valid {
		s := dec(r.Size.Decimal)
		out.Size = &s
	}
	rets := []gen.EtfReturn{}
	for i, v := range []*float64{r.Return1y, r.Return2y, r.Return3y, r.Return4y, r.Return5y, r.Return6y, r.Return7y, r.Return8y, r.Return9y, r.Return10y} {
		if v != nil {
			rets = append(rets, gen.EtfReturn{Years: i + 1, Annualized: *v})
		}
	}
	if len(rets) > 0 {
		out.Returns = &rets
	}
	return out
}
