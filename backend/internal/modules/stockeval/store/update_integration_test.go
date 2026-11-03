//go:build integration

package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

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
