package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/store/etfdb"
)

// MaxModelHoldings est le nombre maximal de fonds d'un modèle (D324).
const MaxModelHoldings = 20

// Model est un portefeuille modèle dans sa dernière version (D313).
type Model struct {
	ID           uuid.UUID
	Name         string
	Version      int
	Targets      ingest.Allocation
	CashTarget   float64
	BandRelative float64
	BandCash     float64
	Holdings     []ingest.ModelHolding
	CreatedAt    time.Time
}

func modelOf(m etfdb.EtfModel, v etfdb.EtfModelVersion, hs []etfdb.ListModelHoldingsRow) Model {
	out := Model{
		ID: m.ID, Name: m.Name, Version: int(v.Version),
		Targets: ingest.Allocation{
			EquityCA: v.EquityCa, EquityUS: v.EquityUs, EquityIntl: v.EquityIntl,
			BondCA: v.BondCa, BondOther: v.BondOther, OtherAssets: v.OtherAssets,
		},
		CashTarget: v.CashTarget, BandRelative: v.BandRelative, BandCash: v.BandCashPoints,
		Holdings: make([]ingest.ModelHolding, 0, len(hs)), CreatedAt: v.CreatedAt,
	}
	for _, h := range hs {
		out.Holdings = append(out.Holdings, ingest.ModelHolding{Symbol: h.Symbol, Weight: h.Weight})
	}
	return out
}

func latest(ctx context.Context, q *etfdb.Queries, owner uuid.UUID, m etfdb.EtfModel) (Model, error) {
	v, err := q.GetLatestModelVersion(ctx, etfdb.GetLatestModelVersionParams{OwnerID: owner, ModelID: m.ID})
	if err != nil {
		return Model{}, fmt.Errorf("lire la version du modèle: %w", err)
	}
	hs, err := q.ListModelHoldings(ctx, etfdb.ListModelHoldingsParams{OwnerID: owner, ModelID: m.ID, Version: v.Version})
	if err != nil {
		return Model{}, fmt.Errorf("lire les fonds du modèle: %w", err)
	}
	return modelOf(m, v, hs), nil
}

// ListModels rend la dernière version de chaque modèle du propriétaire.
func (s *Store) ListModels(ctx context.Context, owner uuid.UUID) ([]Model, error) {
	q := etfdb.New(s.core.Pool())
	models, err := q.ListModels(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("lister les modèles: %w", err)
	}
	out := make([]Model, 0, len(models))
	for _, m := range models {
		mv, err := latest(ctx, q, owner, m)
		if err != nil {
			return nil, err
		}
		out = append(out, mv)
	}
	return out, nil
}

// GetModel rend la dernière version d'un modèle du propriétaire (NotFoundError sinon).
func (s *Store) GetModel(ctx context.Context, owner, id uuid.UUID) (Model, error) {
	q := etfdb.New(s.core.Pool())
	m, err := q.GetModel(ctx, etfdb.GetModelParams{OwnerID: owner, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Model{}, &corestore.NotFoundError{Message: "Modèle introuvable."}
	}
	if err != nil {
		return Model{}, fmt.Errorf("lire le modèle: %w", err)
	}
	return latest(ctx, q, owner, m)
}

// CreateModel crée un modèle à la version 1 ; un nom déjà pris donne ErrConflict.
func (s *Store) CreateModel(ctx context.Context, owner uuid.UUID, in ingest.Model) (Model, error) {
	var out Model
	err := s.core.InTx(ctx, func(tx pgx.Tx) error {
		cq, eq := coredb.New(tx), etfdb.New(tx)
		m, err := eq.CreateModel(ctx, etfdb.CreateModelParams{OwnerID: owner, Name: in.Name})
		if err != nil {
			return corestore.MapPg(fmt.Errorf("créer le modèle: %w", err))
		}
		if err := corestore.Audit(ctx, cq, owner, "model.create", "etf.model", nil, map[string]any{"id": m.ID, "name": m.Name}); err != nil {
			return err
		}
		out, err = writeVersion(ctx, cq, eq, owner, m, in)
		return err
	})
	return out, err
}

// PublishModelVersion publie la version suivante d'un modèle existant. Le nom ne change pas.
func (s *Store) PublishModelVersion(ctx context.Context, owner, id uuid.UUID, in ingest.Model) (Model, error) {
	var out Model
	err := s.core.InTx(ctx, func(tx pgx.Tx) error {
		cq, eq := coredb.New(tx), etfdb.New(tx)
		m, err := eq.GetModel(ctx, etfdb.GetModelParams{OwnerID: owner, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return &corestore.NotFoundError{Message: "Modèle introuvable."}
		}
		if err != nil {
			return fmt.Errorf("lire le modèle: %w", err)
		}
		if m.Name != in.Name {
			return &corestore.InvalidError{Message: "Le nom d'un modèle ne change pas d'une version à l'autre."}
		}
		out, err = writeVersion(ctx, cq, eq, owner, m, in)
		return err
	})
	return out, err
}

func writeVersion(ctx context.Context, cq *coredb.Queries, eq *etfdb.Queries, owner uuid.UUID, m etfdb.EtfModel, in ingest.Model) (Model, error) {
	if len(in.Holdings) > MaxModelHoldings {
		return Model{}, &corestore.InvalidError{Message: "Un modèle compte 20 fonds au plus."}
	}
	ver, err := eq.InsertModelVersion(ctx, etfdb.InsertModelVersionParams{
		OwnerID: owner, ModelID: m.ID,
		EquityCa: in.Targets.EquityCA, EquityUs: in.Targets.EquityUS, EquityIntl: in.Targets.EquityIntl,
		BondCa: in.Targets.BondCA, BondOther: in.Targets.BondOther, OtherAssets: in.Targets.OtherAssets,
		CashTarget: in.CashTarget, BandRelative: in.BandRelative, BandCashPoints: in.BandCash,
	})
	if err != nil {
		return Model{}, corestore.MapPg(fmt.Errorf("publier la version: %w", err))
	}
	for i, h := range in.Holdings {
		inst, err := cq.GetInstrumentBySymbol(ctx, coredb.GetInstrumentBySymbolParams{OwnerID: owner, Symbol: h.Symbol})
		if errors.Is(err, pgx.ErrNoRows) {
			return Model{}, &corestore.InvalidError{Message: "Un fonds du modèle n'est pas au catalogue."}
		}
		if err != nil {
			return Model{}, fmt.Errorf("chercher le fonds %s: %w", h.Symbol, err)
		}
		if err := eq.InsertModelHolding(ctx, etfdb.InsertModelHoldingParams{
			OwnerID: owner, ModelID: m.ID, Version: ver.Version, Position: int16(i + 1),
			InstrumentID: inst.ID, Weight: h.Weight,
		}); err != nil {
			return Model{}, corestore.MapPg(fmt.Errorf("écrire le fonds %s: %w", h.Symbol, err))
		}
	}
	if err := corestore.Audit(ctx, cq, owner, "model.version", "etf.model_version", nil,
		map[string]any{"modelId": m.ID, "version": ver.Version, "holdings": len(in.Holdings)}); err != nil {
		return Model{}, err
	}
	hs := make([]etfdb.ListModelHoldingsRow, len(in.Holdings))
	for i, h := range in.Holdings {
		hs[i] = etfdb.ListModelHoldingsRow{Symbol: h.Symbol, Weight: h.Weight}
	}
	return modelOf(m, ver, hs), nil
}

// LastPrices rend, par symbole, le dernier prix d'écriture du registre dans la
// devise donnée jusqu'au jour asOf (D410). Un symbole sans prix est absent.
func (s *Store) LastPrices(ctx context.Context, owner uuid.UUID, symbols []string, currency string, asOf time.Time) (map[string]decimal.Decimal, error) {
	out := map[string]decimal.Decimal{}
	if len(symbols) == 0 {
		return out, nil
	}
	rows, err := etfdb.New(s.core.Pool()).LastTradePrices(ctx, etfdb.LastTradePricesParams{
		OwnerID: owner, Currency: currency, AsOf: asOf, Symbols: symbols,
	})
	if err != nil {
		return nil, fmt.Errorf("lire les derniers prix: %w", err)
	}
	for _, r := range rows {
		out[r.Symbol] = r.Price
	}
	return out, nil
}
