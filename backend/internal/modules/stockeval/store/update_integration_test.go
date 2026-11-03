//go:build integration

package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

func TestResolveConflicts(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()
	a := addStock(t, ctx, s, owner, "RSV")
	cad := "CAD"
	w := func(v, src string) {
		t.Helper()
		if _, err := s.WriteFact(ctx, owner, store.Fact{
			InstrumentID: a.ID, FiscalYear: 2024, Metric: store.MetricEPSDiluted, Value: dec(v), Currency: &cad, Source: src,
		}); err != nil {
			t.Fatal(err)
		}
	}
	w("2.50", store.SourceManual)
	w("2.60", store.SourceXLSX)
	if cs, _ := s.ListOpenConflicts(ctx, owner, a.ID); len(cs) != 1 {
		t.Fatalf("conflits = %d, attendu 1", len(cs))
	}
	if n, err := s.ResolveConflicts(ctx, uuid.New(), a.ID, 2024, store.MetricEPSDiluted); !errors.Is(err, store.ErrNotFound) || n != 0 {
		t.Fatalf("autre propriétaire: n=%d err=%v", n, err)
	}
	if n, err := s.ResolveConflicts(ctx, owner, a.ID, 2023, store.MetricEPSDiluted); err != nil || n != 0 {
		t.Fatalf("autre année: n=%d err=%v", n, err)
	}
	before := auditCount(t, owner)
	if n, err := s.ResolveConflicts(ctx, owner, a.ID, 2024, store.MetricEPSDiluted); err != nil || n != 1 {
		t.Fatalf("résolution: n=%d err=%v", n, err)
	}
	if cs, _ := s.ListOpenConflicts(ctx, owner, a.ID); len(cs) != 0 || auditCount(t, owner) != before+1 {
		t.Fatalf("après résolution: %d conflits, audit %d", len(cs), auditCount(t, owner))
	}
	if _, err := s.ResolveConflicts(ctx, owner, a.ID, 1000, store.MetricEPSDiluted); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("année invalide: err = %v", err)
	}
}

func TestUpdateInstrument(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()
	a := addStock(t, ctx, s, owner, "UPD")
	n := auditCount(t, owner)

	u := store.InstrumentUpdate{Name: "Nouveau nom", QuoteCurrency: "USD", ReportCurrency: "CAD", IsFinancial: true}
	got, err := s.UpdateInstrument(ctx, owner, a.ID, u)
	if err != nil || got.Name != "Nouveau nom" || got.QuoteCurrency != "USD" || !got.IsFinancial {
		t.Fatalf("UpdateInstrument = %+v, %v", got, err)
	}
	if auditCount(t, owner) != n+1 {
		t.Fatalf("audit = %d, attendu %d", auditCount(t, owner), n+1)
	}
	if _, err := s.UpdateInstrument(ctx, owner, a.ID, u); err != nil || auditCount(t, owner) != n+1 {
		t.Fatalf("modification identique: err=%v audit=%d", err, auditCount(t, owner))
	}
	if _, err := s.UpdateInstrument(ctx, uuid.New(), a.ID, u); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("autre propriétaire: err = %v", err)
	}
	bad := store.InstrumentUpdate{Name: "x", QuoteCurrency: "CA", ReportCurrency: "CAD"}
	if _, err := s.UpdateInstrument(ctx, owner, a.ID, bad); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("devise invalide: err = %v", err)
	}
}
