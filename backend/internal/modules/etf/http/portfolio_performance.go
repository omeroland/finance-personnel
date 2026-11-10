package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

func nd(d decimal.NullDecimal) decimal.Decimal {
	if d.Valid {
		return d.Decimal
	}
	return decimal.Zero
}

func toEntry(t corestore.Tx, account uuid.UUID) engine.Entry {
	return engine.Entry{
		Date: t.TradeDate, Type: t.Type, Symbol: t.Symbol,
		Quantity: nd(t.Quantity), Price: nd(t.Price), Amount: nd(t.Amount), Fee: nd(t.Fee), SplitRatio: nd(t.SplitRatio),
		Incoming: t.AccountID != account,
	}
}

// statementSeries sépare le premier relevé du lot (valeur de départ, D413) du reste de la série.
func statementSeries(rows []corestore.Statement, fromMonth, toMonth string) (engine.StatementAccount, bool) {
	var in []corestore.Statement
	for _, s := range rows {
		if s.Month >= fromMonth && s.Month <= toMonth {
			in = append(in, s)
		}
	}
	if len(in) < 2 {
		return engine.StatementAccount{}, false
	}
	acc := engine.StatementAccount{Start: in[0].EndValue}
	for _, s := range in[1:] {
		acc.Statements = append(acc.Statements, engine.Statement{Month: s.Month, Contribution: nd(s.Contribution), EndValue: s.EndValue})
	}
	return acc, true
}

// GetPerformance : rendement pondéré dans le temps et TRI, consolidé et par compte.
//
// Un compte avec transactions (method `transactions`) donne un TRI annualisé (XIRR sur les flux datés).
// Un compte sans transactions (method `statements`) donne un TRI cumulé sur la période, l'AACJ de l'outil
// (annualisé seulement à 12 mois, D337, D413). Le champ `method` indique laquelle des deux bases
// s'applique ; les rendements annuels se déduisent de la même série que les mois (D381).
func (h *Handler) GetPerformance(ctx context.Context, req gen.GetPerformanceRequestObject) (gen.GetPerformanceResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	to := h.today()
	var fe fieldErrors
	var fromSet bool
	var from time.Time
	if p := req.Params.From; p != nil {
		from, fromSet = p.Time, true
		if !validDate(from) {
			fe.add("from", "La date doit être comprise entre 1900 et 2100.")
		}
	}
	if p := req.Params.To; p != nil {
		to = p.Time
		if !validDate(to) {
			fe.add("to", "La date doit être comprise entre 1900 et 2100.")
		}
	}
	if fromSet && to.Before(from) {
		fe.add("from", "Le début doit précéder la fin.")
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	accounts, err := h.resolveAccounts(ctx, owner, accountIDs(req.Params.Accounts))
	if err != nil {
		return nil, err
	}

	var txAccounts []engine.LedgerAccount
	statements := map[uuid.UUID][]corestore.Statement{}
	var order []uuid.UUID
	method := map[uuid.UUID]gen.PerformanceMethod{}
	for _, a := range accounts {
		entries, err := h.ledger.Entries(ctx, owner, a.ID, to)
		if err != nil {
			return nil, translate(fmt.Errorf("lire le registre: %w", err))
		}
		order = append(order, a.ID)
		if len(entries) > 0 {
			la := engine.LedgerAccount{ID: a.ID.String()}
			for _, t := range entries {
				la.Entries = append(la.Entries, toEntry(t, a.ID))
			}
			txAccounts = append(txAccounts, la)
			method[a.ID] = gen.Transactions
			continue
		}
		rows, err := h.ledger.ListStatements(ctx, owner, a.ID)
		if err != nil {
			return nil, translate(fmt.Errorf("lire les relevés: %w", err))
		}
		if len(rows) > 0 {
			statements[a.ID] = rows
			method[a.ID] = gen.Statements
		}
	}

	if !fromSet {
		from = to
		if first, ok := engine.FirstEntryDate(txAccounts); ok {
			from = first
		}
		for _, rows := range statements {
			if m, err := time.Parse("2006-01", rows[0].Month); err == nil && m.Before(from) {
				from = m
			}
		}
		if from.After(to) {
			from = to
		}
	}
	fromM, toM := from.Format("2006-01"), to.Format("2006-01")

	out := gen.Performance{From: dateOf(from), To: dateOf(to), Accounts: make([]gen.PerformanceSeries, 0, len(order))}
	timelines := map[string][]engine.Valuation{}
	if len(txAccounts) > 0 {
		timelines, err = engine.AccountTimelines(txAccounts, from, to)
		if err != nil {
			return nil, httpx.BadRequest("La période demandée est invalide.")
		}
	}
	var stmtAccounts []engine.StatementAccount
	stmtOK := true
	var txPoints [][]engine.Valuation
	for _, id := range order {
		var perf engine.Performance
		var ok bool
		switch method[id] {
		case gen.Transactions:
			pts := timelines[id.String()]
			txPoints = append(txPoints, pts)
			perf, err = engine.TWR(pts)
			ok = err == nil
		case gen.Statements:
			if sa, has := statementSeries(statements[id], fromM, toM); has {
				stmtAccounts = append(stmtAccounts, sa)
				perf, err = engine.StatementPerformance(sa.Start, sa.Statements)
				ok = err == nil
			} else {
				stmtOK = false
			}
		}
		if err != nil {
			h.log.WarnContext(ctx, "rendement d'un compte non calculable", "error", err.Error())
		}
		aid := id
		out.Accounts = append(out.Accounts, seriesDTO(&aid, method[id], perf, ok))
	}

	// Consolidé : comptes de même méthode et de même horizon seulement (D413).
	var cons engine.Performance
	consOK := false
	var consMethod gen.PerformanceMethod
	switch {
	case len(txPoints) > 0 && len(statements) == 0:
		consMethod = gen.Transactions
		if merged, err := engine.MergeValuations(txPoints); err == nil {
			if cons, err = engine.TWR(merged); err == nil {
				consOK = true
			}
		}
	case len(txPoints) == 0 && len(statements) > 0 && stmtOK:
		consMethod = gen.Statements
		var err error
		if cons, err = engine.ConsolidateStatements(stmtAccounts); err == nil {
			consOK = true
		}
	}
	out.Consolidated = seriesDTO(nil, consMethod, cons, consOK)
	return gen.GetPerformance200JSONResponse(out), nil
}

func seriesDTO(id *uuid.UUID, method gen.PerformanceMethod, p engine.Performance, ok bool) gen.PerformanceSeries {
	s := gen.PerformanceSeries{AccountId: id, Monthly: []gen.PerformancePoint{}, Annual: []gen.PerformancePoint{}}
	if !ok {
		return s
	}
	m := method
	s.Method = &m
	if p.HasCumulative {
		v := p.Cumulative
		s.Twr = &v
	}
	if p.HasIRR {
		v := p.IRR
		s.Irr = &v
	}
	for _, r := range p.Monthly {
		pt := gen.PerformancePoint{Period: r.Month}
		if r.Defined {
			v := r.Return
			pt.Twr = &v
		}
		if r.HasAACJ {
			v := r.AACJ
			pt.Irr = &v
		}
		s.Monthly = append(s.Monthly, pt)
	}
	for _, y := range p.Annual() {
		v := y.Return
		s.Annual = append(s.Annual, gen.PerformancePoint{Period: fmt.Sprintf("%04d", y.Year), Twr: &v})
	}
	return s
}
