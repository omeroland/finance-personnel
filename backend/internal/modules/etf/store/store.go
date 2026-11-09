// Package store contient les dépôts PostgreSQL du module 2 (FNB) : catalogue,
// modèles et implémentations des dépôts d'import (ingest.Repository et
// ingest.TransactionRepository). Même règles que le socle : owner_id partout,
// écritures en transaction avec une ligne d'audit.
package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/store/etfdb"
)

// Store implémente les dépôts du module au-dessus du dépôt du socle.
type Store struct{ core *corestore.Store }

// New construit le dépôt.
func New(core *corestore.Store) *Store { return &Store{core: core} }

var (
	_ ingest.Repository            = (*Store)(nil)
	_ ingest.TransactionRepository = (*Store)(nil)
)

// Catalog est un FNB du catalogue avec son dernier profil.
type Catalog = etfdb.ListLatestEtfProfilesRow

// CatalogFilter filtre le catalogue ; un champ vide ne filtre pas.
type CatalogFilter struct {
	Type   string
	Region string
	MaxMER *float64
}

// ListCatalog rend le dernier profil de chaque FNB du propriétaire.
func (s *Store) ListCatalog(ctx context.Context, owner uuid.UUID, f CatalogFilter) ([]Catalog, error) {
	rows, err := etfdb.New(s.core.Pool()).ListLatestEtfProfiles(ctx, etfdb.ListLatestEtfProfilesParams{OwnerID: owner, MaxMer: f.MaxMER})
	if err != nil {
		return nil, fmt.Errorf("lister le catalogue: %w", err)
	}
	out := make([]Catalog, 0, len(rows))
	for _, r := range rows {
		if f.Type != "" && r.Type != f.Type {
			continue
		}
		if f.Region != "" && (r.Region == nil || *r.Region != f.Region) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func profileParams(owner, inst uuid.UUID, fp ingest.FundProfile) etfdb.UpsertEtfProfileParams {
	ret := func(y int) *float64 {
		if v, ok := fp.Returns[y]; ok {
			return &v
		}
		return nil
	}
	p := etfdb.UpsertEtfProfileParams{
		OwnerID: owner, InstrumentID: inst, AsOf: fp.AsOf, Type: fp.Type, Region: fp.Region,
		EquityCa: fp.Alloc.EquityCA, EquityUs: fp.Alloc.EquityUS, EquityIntl: fp.Alloc.EquityIntl,
		BondCa: fp.Alloc.BondCA, BondOther: fp.Alloc.BondOther, OtherAssets: fp.Alloc.OtherAssets,
		Mer: fp.MER, Esg: fp.ESG, Source: fp.Source,
		Return1y: ret(1), Return2y: ret(2), Return3y: ret(3), Return4y: ret(4), Return5y: ret(5),
		Return6y: ret(6), Return7y: ret(7), Return8y: ret(8), Return9y: ret(9), Return10y: ret(10),
	}
	if fp.Size != nil {
		p.Size.Decimal, p.Size.Valid = fp.Size.Round(6), true
	}
	if p.Source == "" {
		p.Source = "xlsx"
	}
	return p
}

func eqF(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return math.Abs(*a-*b) < 1e-12
}

func sameProfile(cur etfdb.EtfEtfProfile, p etfdb.UpsertEtfProfileParams) bool {
	if cur.Type != p.Type || cur.Source != p.Source || cur.Size.Valid != p.Size.Valid ||
		(cur.Size.Valid && !cur.Size.Decimal.Equal(p.Size.Decimal)) {
		return false
	}
	if (cur.Region == nil) != (p.Region == nil) || (cur.Region != nil && *cur.Region != *p.Region) {
		return false
	}
	if (cur.Esg == nil) != (p.Esg == nil) || (cur.Esg != nil && *cur.Esg != *p.Esg) {
		return false
	}
	pairs := [][2]float64{
		{cur.EquityCa, p.EquityCa},
		{cur.EquityUs, p.EquityUs},
		{cur.EquityIntl, p.EquityIntl},
		{cur.BondCa, p.BondCa},
		{cur.BondOther, p.BondOther},
		{cur.OtherAssets, p.OtherAssets},
	}
	for _, x := range pairs {
		if math.Abs(x[0]-x[1]) >= 1e-12 {
			return false
		}
	}
	return eqF(cur.Mer, p.Mer) && eqF(cur.Return1y, p.Return1y) && eqF(cur.Return2y, p.Return2y) &&
		eqF(cur.Return3y, p.Return3y) && eqF(cur.Return4y, p.Return4y) && eqF(cur.Return5y, p.Return5y) &&
		eqF(cur.Return6y, p.Return6y) && eqF(cur.Return7y, p.Return7y) && eqF(cur.Return8y, p.Return8y) &&
		eqF(cur.Return9y, p.Return9y) && eqF(cur.Return10y, p.Return10y)
}

// SaveProfiles crée l'instrument au besoin et écrit le profil sous (instrument, as_of).
func (s *Store) SaveProfiles(ctx context.Context, owner uuid.UUID, profiles []ingest.FundProfile) (ingest.SaveCounts, error) {
	var c ingest.SaveCounts
	err := s.core.InTx(ctx, func(tx pgx.Tx) error {
		cq, eq := coredb.New(tx), etfdb.New(tx)
		for _, fp := range profiles {
			inst, err := cq.GetInstrumentBySymbol(ctx, coredb.GetInstrumentBySymbolParams{OwnerID: owner, Symbol: fp.Symbol})
			if errors.Is(err, pgx.ErrNoRows) {
				exchange := fp.Exchange
				if exchange == "" {
					exchange, _, _ = strings.Cut(fp.Symbol, ":")
				}
				inst, err = cq.InsertInstrument(ctx, coredb.InsertInstrumentParams{
					OwnerID: owner, Symbol: fp.Symbol, Name: fp.Name, Exchange: exchange, Kind: "etf",
					QuoteCurrency: fp.Currency, ReportCurrency: fp.Currency,
				})
				if err == nil {
					err = corestore.Audit(ctx, cq, owner, "instrument.create", "core.instrument", nil,
						map[string]any{"id": inst.ID, "symbol": inst.Symbol})
				}
			}
			if err != nil {
				return corestore.MapPg(fmt.Errorf("préparer l'instrument %s: %w", fp.Symbol, err))
			}
			p := profileParams(owner, inst.ID, fp)
			cur, err := eq.GetEtfProfileAt(ctx, etfdb.GetEtfProfileAtParams{OwnerID: owner, InstrumentID: inst.ID, AsOf: fp.AsOf})
			exists := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("lire le profil %s: %w", fp.Symbol, err)
			}
			if exists && sameProfile(cur, p) {
				c.Unchanged++
				continue
			}
			if err := eq.UpsertEtfProfile(ctx, p); err != nil {
				return corestore.MapPg(fmt.Errorf("écrire le profil %s: %w", fp.Symbol, err))
			}
			action := "etf_profile.create"
			if exists {
				action = "etf_profile.update"
				c.Updated++
			} else {
				c.Created++
			}
			if err := corestore.Audit(ctx, cq, owner, action, "etf.etf_profile", nil,
				map[string]any{"instrumentId": inst.ID, "asOf": fp.AsOf.Format(time.DateOnly), "source": p.Source}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ingest.SaveCounts{}, err
	}
	return c, nil
}

func sameModel(v etfdb.EtfModelVersion, hs []etfdb.ListModelHoldingsRow, m ingest.Model) bool {
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-12 }
	if !near(v.EquityCa, m.Targets.EquityCA) || !near(v.EquityUs, m.Targets.EquityUS) ||
		!near(v.EquityIntl, m.Targets.EquityIntl) || !near(v.BondCa, m.Targets.BondCA) ||
		!near(v.BondOther, m.Targets.BondOther) || !near(v.OtherAssets, m.Targets.OtherAssets) ||
		!near(v.CashTarget, m.CashTarget) || !near(v.BandRelative, m.BandRelative) ||
		!near(v.BandCashPoints, m.BandCash) || len(hs) != len(m.Holdings) {
		return false
	}
	for i, h := range hs {
		if h.Symbol != m.Holdings[i].Symbol || !near(h.Weight, m.Holdings[i].Weight) {
			return false
		}
	}
	return true
}

// SaveModel publie une nouvelle version du modèle de même nom, sauf si la
// dernière a exactement le même contenu.
func (s *Store) SaveModel(ctx context.Context, owner uuid.UUID, m ingest.Model) (string, error) {
	if len(m.Holdings) > 20 {
		return "", &corestore.InvalidError{Message: "Un modèle compte 20 fonds au plus."}
	}
	status := ingest.ModelCreated
	err := s.core.InTx(ctx, func(tx pgx.Tx) error {
		cq, eq := coredb.New(tx), etfdb.New(tx)
		model, err := eq.GetModelByName(ctx, etfdb.GetModelByNameParams{OwnerID: owner, Name: m.Name})
		if errors.Is(err, pgx.ErrNoRows) {
			model, err = eq.CreateModel(ctx, etfdb.CreateModelParams{OwnerID: owner, Name: m.Name})
			if err == nil {
				err = corestore.Audit(ctx, cq, owner, "model.create", "etf.model", nil, map[string]any{"id": model.ID, "name": model.Name})
			}
		} else if err == nil {
			cur, verr := eq.GetLatestModelVersion(ctx, etfdb.GetLatestModelVersionParams{OwnerID: owner, ModelID: model.ID})
			if verr == nil {
				hs, herr := eq.ListModelHoldings(ctx, etfdb.ListModelHoldingsParams{OwnerID: owner, ModelID: model.ID, Version: cur.Version})
				if herr != nil {
					return fmt.Errorf("lire les fonds du modèle: %w", herr)
				}
				if sameModel(cur, hs, m) {
					status = ingest.ModelUnchanged
					return nil
				}
			} else if !errors.Is(verr, pgx.ErrNoRows) {
				return fmt.Errorf("lire la version du modèle: %w", verr)
			}
		}
		if err != nil {
			return corestore.MapPg(fmt.Errorf("préparer le modèle: %w", err))
		}
		ver, err := eq.InsertModelVersion(ctx, etfdb.InsertModelVersionParams{
			OwnerID: owner, ModelID: model.ID,
			EquityCa: m.Targets.EquityCA, EquityUs: m.Targets.EquityUS, EquityIntl: m.Targets.EquityIntl,
			BondCa: m.Targets.BondCA, BondOther: m.Targets.BondOther, OtherAssets: m.Targets.OtherAssets,
			CashTarget: m.CashTarget, BandRelative: m.BandRelative, BandCashPoints: m.BandCash,
		})
		if err != nil {
			return corestore.MapPg(fmt.Errorf("publier la version: %w", err))
		}
		for i, h := range m.Holdings {
			inst, err := cq.GetInstrumentBySymbol(ctx, coredb.GetInstrumentBySymbolParams{OwnerID: owner, Symbol: h.Symbol})
			if errors.Is(err, pgx.ErrNoRows) {
				return &corestore.InvalidError{Message: "Un fonds du modèle n'est pas au catalogue."}
			}
			if err != nil {
				return fmt.Errorf("chercher le fonds %s: %w", h.Symbol, err)
			}
			if err := eq.InsertModelHolding(ctx, etfdb.InsertModelHoldingParams{
				OwnerID: owner, ModelID: model.ID, Version: ver.Version, Position: int16(i + 1),
				InstrumentID: inst.ID, Weight: h.Weight,
			}); err != nil {
				return corestore.MapPg(fmt.Errorf("écrire le fonds %s: %w", h.Symbol, err))
			}
		}
		return corestore.Audit(ctx, cq, owner, "model.version", "etf.model_version", nil,
			map[string]any{"modelId": model.ID, "version": ver.Version, "holdings": len(m.Holdings)})
	})
	if err != nil {
		return "", err
	}
	return status, nil
}

// SaveTransactions écrit les lignes d'un import CSV (D392) ; voir ingest.TransactionRepository.
func (s *Store) SaveTransactions(ctx context.Context, owner uuid.UUID, txs []ingest.Transaction) ([]ingest.Outcome, error) {
	rows := make([]corestore.ImportRow, len(txs))
	for i, t := range txs {
		rows[i] = corestore.ImportRow{
			Account: t.Account, Counterpart: t.Counterpart, Type: t.Type, Date: t.Date, Symbol: t.Symbol,
			Quantity: t.Quantity, Price: t.Price, Amount: t.Amount, Fee: t.Fee, SplitRatio: t.SplitRatio,
			Currency: t.Currency, Note: t.Note, Source: t.Source, Occurrence: t.Occurrence,
		}
	}
	res, err := s.core.ImportTransactions(ctx, owner, rows)
	if err != nil {
		return nil, err
	}
	out := make([]ingest.Outcome, len(res))
	for i, r := range res {
		out[i] = ingest.Outcome{Status: r.Status, Message: r.Message}
	}
	return out, nil
}
