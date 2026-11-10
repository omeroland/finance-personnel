package httpapi

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// CreatePurchasePlan : plan d'achat initial d'un modèle (calculé, non enregistré, D314).
// Les prix viennent de la demande (D440) ou, à défaut, du dernier prix d'écriture du registre (D410) ;
// un prix manquant donne 422 avec la liste des fonds.
func (h *Handler) CreatePurchasePlan(ctx context.Context, req gen.CreatePurchasePlanRequestObject) (gen.CreatePurchasePlanResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	b := *req.Body
	var fe fieldErrors
	fe.currency("currency", b.Currency)
	amount := fe.amount("amount", &b.Amount, true)
	manual := manualPrices(&fe, b.Prices)
	if err := fe.err(); err != nil {
		return nil, err
	}
	m, err := h.pf.GetModel(ctx, owner, req.Id)
	if err != nil {
		return nil, translate(fmt.Errorf("lire le modèle: %w", err))
	}
	symbols := make([]string, len(m.Holdings))
	for i, hd := range m.Holdings {
		symbols[i] = hd.Symbol
	}
	prices, err := h.pf.LastPrices(ctx, owner, symbols, string(b.Currency), h.today())
	if err != nil {
		return nil, translate(fmt.Errorf("lire les prix: %w", err))
	}
	if prices == nil {
		prices = map[string]decimal.Decimal{}
	}
	var noPrice []string
	for _, hd := range m.Holdings {
		if p, ok := manual[hd.Symbol]; ok {
			prices[hd.Symbol] = p
		}
		if p, ok := prices[hd.Symbol]; !ok || !p.IsPositive() {
			noPrice = append(noPrice, hd.Symbol)
		}
	}
	if len(noPrice) > 0 {
		return nil, httpx.Unprocessable("Prix manquant pour : " + missingList(noPrice) + ". Saisissez un prix par fonds dans la demande.")
	}
	in := engine.PurchaseInput{Amount: *amount, CashWeight: m.CashTarget}
	for _, hd := range m.Holdings {
		in.Funds = append(in.Funds, engine.PurchaseFund{ID: hd.Symbol, Weight: hd.Weight, Price: prices[hd.Symbol]})
	}
	plan, err := engine.PlanPurchase(in)
	if err != nil {
		return nil, planError(fmt.Errorf("plan d'achat: %w", err))
	}
	out := gen.PurchasePlan{
		ModelId: m.ID, ModelVersion: m.Version, Currency: b.Currency, Amount: dec(*amount),
		Invested: dec(plan.Invested), CashRemainder: dec(plan.Cash), Orders: make([]gen.PlanOrder, 0, len(plan.Orders)),
	}
	for _, o := range plan.Orders {
		out.Orders = append(out.Orders, gen.PlanOrder{
			Side: gen.OrderSide(engine.SideBuy), Symbol: o.Fund, Shares: int(o.Shares), Price: dec(o.Price), Amount: dec(o.Amount),
		})
	}
	return gen.CreatePurchasePlan200JSONResponse(out), nil
}

// CreateRebalancePlan : plan de rééquilibrage d'un ou plusieurs comptes (calculé, non enregistré, D314).
// La cotisation se donne par compte (`contributions`, D443) ; l'ancien champ `contribution` va au premier
// compte retenu pour le plan (D414). Les prix saisis remplacent ceux du registre (D440).
func (h *Handler) CreateRebalancePlan(ctx context.Context, req gen.CreateRebalancePlanRequestObject) (gen.CreateRebalancePlanResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	b := *req.Body
	var fe fieldErrors
	fe.currency("currency", b.Currency)
	contribution := fe.amount("contribution", b.Contribution, false)
	minOrder := fe.amount("minOrderAmount", b.MinOrderAmount, false)
	manual := manualPrices(&fe, b.Prices)
	contribs := map[uuid.UUID]decimal.Decimal{}
	if b.Contributions != nil {
		if b.Contribution != nil {
			fe.add("contribution", "Champ obsolète : utiliser seulement `contributions`.")
		}
		for i, c := range *b.Contributions {
			field := fmt.Sprintf("contributions[%d]", i)
			if _, dup := contribs[c.AccountId]; dup {
				fe.add(field, "Un compte ne figure qu'une fois.")
				continue
			}
			amt := c.Amount
			if v := fe.amount(field+".amount", &amt, false); v != nil {
				contribs[c.AccountId] = *v
			}
		}
	}
	if b.Bands != nil {
		for _, v := range []*float64{b.Bands.Relative, b.Bands.CashPoints} {
			if v != nil && !(*v >= 0 && *v <= 1) {
				fe.add("bands", "Les bandes sont des fractions entre 0 et 1.")
			}
		}
	}
	var ids []uuid.UUID
	if b.AccountIds != nil {
		ids = *b.AccountIds
		if len(ids) > maxAccountsParam {
			fe.add("accountIds", "50 comptes au plus.")
		}
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	m, err := h.pf.GetModel(ctx, owner, b.ModelId)
	if err != nil {
		return nil, translate(fmt.Errorf("lire le modèle: %w", err))
	}
	accounts, err := h.resolveAccounts(ctx, owner, ids)
	if err != nil {
		return nil, err
	}
	explicit := len(ids) > 0
	today := h.today()
	var rebAccounts []engine.RebalAccount
	used := map[uuid.UUID]bool{}
	symbols := map[string]bool{}
	for _, hd := range m.Holdings {
		symbols[hd.Symbol] = true
	}
	for _, a := range accounts {
		if a.Currency != string(b.Currency) {
			if explicit {
				return nil, httpx.BadRequest("Un compte demandé n'est pas dans la devise du plan (pas de conversion en phase 2).")
			}
			continue // vue « tous les comptes » : seuls les comptes de la devise du plan (D414)
		}
		hold, err := h.ledger.Holdings(ctx, owner, a.ID, today)
		if err != nil {
			return nil, translate(fmt.Errorf("lire les positions: %w", err))
		}
		ra := engine.RebalAccount{ID: a.ID.String(), Type: engine.AccountType(a.Type), Cash: hold.Cash}
		for _, p := range hold.Positions {
			symbols[p.Symbol] = true
			ra.Positions = append(ra.Positions, engine.RebalPosition{FundID: p.Symbol, Shares: p.Quantity})
		}
		if c, ok := contribs[a.ID]; ok {
			ra.Contribution = c
			used[a.ID] = true
		} else if len(rebAccounts) == 0 && contribution != nil {
			ra.Contribution = *contribution
		}
		rebAccounts = append(rebAccounts, ra)
	}
	for id := range contribs {
		if !used[id] {
			return nil, httpx.BadRequest("Une cotisation vise un compte absent du plan (autre devise, autre propriétaire ou hors accountIds).")
		}
	}
	list := make([]string, 0, len(symbols))
	for s := range symbols {
		list = append(list, s)
	}
	sort.Strings(list)
	prices, err := h.pf.LastPrices(ctx, owner, list, string(b.Currency), today)
	if err != nil {
		return nil, translate(fmt.Errorf("lire les prix: %w", err))
	}
	profiles, err := h.profiles(ctx, owner)
	if err != nil {
		return nil, err
	}
	targets := map[string]float64{}
	for _, hd := range m.Holdings {
		targets[hd.Symbol] = hd.Weight
	}
	if prices == nil {
		prices = map[string]decimal.Decimal{}
	}
	for s, p := range manual {
		if symbols[s] {
			prices[s] = p
		}
	}
	var noPrice, noProfile []string
	funds := make([]engine.RebalFund, 0, len(list))
	for _, s := range list {
		p, ok := profiles[s]
		price, hasPrice := prices[s]
		if !hasPrice {
			noPrice = append(noPrice, s)
		}
		if !ok {
			noProfile = append(noProfile, s)
			continue
		}
		alloc, err := fullAllocation(p)
		if err != nil {
			return nil, planError(err)
		}
		funds = append(funds, engine.RebalFund{ID: s, Price: price, Target: targets[s], Allocation: alloc})
	}
	if len(noPrice) > 0 {
		return nil, httpx.Unprocessable("Prix manquant pour : " + missingList(noPrice) + ". Saisissez un prix par fonds dans la demande.")
	}
	if len(noProfile) > 0 {
		return nil, httpx.Unprocessable("Profil absent du catalogue pour : " + missingList(noProfile) + ".")
	}

	bands := engine.Bands{Relative: m.BandRelative, CashPoints: m.BandCash}
	if b.Bands != nil {
		if b.Bands.Relative != nil {
			bands.Relative = *b.Bands.Relative
		}
		if b.Bands.CashPoints != nil {
			bands.CashPoints = *b.Bands.CashPoints
		}
	}
	classT := engineAlloc(m.Targets)
	in := engine.RebalanceInput{
		Funds: funds, Accounts: rebAccounts, CashTarget: m.CashTarget, Bands: &bands, ClassTargets: &classT,
	}
	if minOrder != nil {
		in.MinOrder = *minOrder
	}
	plan, err := engine.Rebalance(in)
	if err != nil {
		return nil, planError(fmt.Errorf("rééquilibrage: %w", err))
	}
	return gen.CreateRebalancePlan200JSONResponse(rebalanceDTO(m.ID, m.Version, b.Currency, classT, plan)), nil
}

func rebalanceDTO(id uuid.UUID, version int, cur gen.Currency, classT engine.Allocation, plan engine.RebalancePlan) gen.RebalancePlan {
	out := gen.RebalancePlan{
		ModelId: id, ModelVersion: version, Currency: cur, CashAfter: dec(plan.EndCash),
		Classes:     make([]gen.ClassStatus, 0, len(allClasses)),
		Funds:       make([]gen.FundStatus, 0, len(plan.Before.Funds)),
		AccountCash: make([]gen.AccountCashAfter, 0, len(plan.AccountCash)),
		Orders:      make([]gen.PlanOrder, 0, len(plan.Orders)),
		Skipped:     make([]gen.SkippedOrder, 0, len(plan.Skipped)),
	}
	if plan.Total.IsPositive() {
		t := dec(plan.Total)
		out.TotalValue = &t
	}
	// Constats du moteur : « kind:id » -> hors bande (avant : OutOfBand ; après : OutOfBandAfter).
	band := func(list []engine.BandStatus) map[string]bool {
		m := map[string]bool{}
		for _, s := range list {
			m[s.Kind+":"+s.ID] = true
		}
		return m
	}
	before, after := band(plan.OutOfBand), band(plan.OutOfBandAfter)
	for _, c := range allClasses {
		out.Classes = append(out.Classes, gen.ClassStatus{
			AssetClass: gen.AssetClass(c), Actual: plan.Before.Classes.Get(c), Target: classT.Get(c),
			OutOfBand: before["class:"+string(c)], After: plan.After.Classes.Get(c), OutOfBandAfter: after["class:"+string(c)],
		})
	}
	for i, f := range plan.Before.Funds {
		fs := gen.FundStatus{
			Symbol: f.FundID, Before: f.Weight, Target: f.Target,
			OutOfBandBefore: before["fund:"+f.FundID], OutOfBandAfter: after["fund:"+f.FundID],
		}
		if i < len(plan.After.Funds) {
			fs.After = plan.After.Funds[i].Weight
		}
		out.Funds = append(out.Funds, fs)
	}
	for _, a := range plan.AccountCash {
		if aid, err := uuid.Parse(a.AccountID); err == nil {
			out.AccountCash = append(out.AccountCash, gen.AccountCashAfter{AccountId: aid, Cash: dec(a.Cash)})
		}
	}
	for _, o := range plan.Orders {
		po := gen.PlanOrder{Side: gen.OrderSide(o.Side), Symbol: o.FundID, Shares: int(o.Shares), Price: dec(o.Price), Amount: dec(o.Amount)}
		if aid, err := uuid.Parse(o.AccountID); err == nil {
			po.AccountId = &aid
		}
		out.Orders = append(out.Orders, po)
	}
	for _, s := range plan.Skipped {
		out.Skipped = append(out.Skipped, gen.SkippedOrder{
			Side: gen.OrderSide(s.Side), Symbol: s.FundID, Amount: dec(s.Amount), Reason: gen.SkippedOrderReason(s.Reason),
		})
	}
	return out
}
