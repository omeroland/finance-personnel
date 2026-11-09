//go:build integration

package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
)

const ownerA = "00000000-0000-0000-0000-00000000000a"
const ownerB = "00000000-0000-0000-0000-00000000000b"

func dec(s string) decimal.NullDecimal {
	return decimal.NullDecimal{Decimal: decimal.RequireFromString(s), Valid: true}
}

func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	adminDSN, appDSN := startPostgres(t)
	admin := openDB(t, adminDSN)
	if _, err := newProvider(t, admin).Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `ALTER ROLE finance_app LOGIN PASSWORD '`+appPassword+`'`); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := coredb.New(pool)
	owner := uuid.MustParse(ownerA)

	var ins [2]uuid.UUID
	for i, sym := range []string{"XEQT.TO", "VAB.TO"} {
		if err := admin.QueryRowContext(ctx, `
			INSERT INTO core.instrument (owner_id, symbol, name, exchange, kind, quote_currency, report_currency)
			VALUES ($1, $2, $2, 'TSX', 'etf', 'CAD', 'CAD') RETURNING id`, ownerA, sym).Scan(&ins[i]); err != nil {
			t.Fatal(err)
		}
	}
	acc, err := q.CreateAccount(ctx, coredb.CreateAccountParams{OwnerID: owner, Name: "CELI", Type: "celi", Broker: "X", Currency: "CAD"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	acc2, err := q.CreateAccount(ctx, coredb.CreateAccountParams{OwnerID: owner, Name: "REER", Type: "reer", Broker: "X", Currency: "CAD"})
	if err != nil {
		t.Fatal(err)
	}
	xeqt, vab := ins[0], ins[1]

	insert := func(typ, date string, inst uuid.UUID, qty, ratio string) coredb.CoreTransaction {
		t.Helper()
		p := coredb.InsertTransactionParams{OwnerID: owner, AccountID: acc.ID, Type: typ, TradeDate: day(date),
			InstrumentID: &inst, Currency: "CAD", Source: "manual"}
		if qty != "" {
			p.Quantity = dec(qty)
			p.Price = dec("10")
		}
		if ratio != "" {
			p.SplitRatio = dec(ratio)
		}
		tx, err := q.InsertTransaction(ctx, p)
		if err != nil {
			t.Fatalf("InsertTransaction %s: %v", typ, err)
		}
		return tx
	}

	insert("buy", "2024-01-10", xeqt, "10.5", "")
	insert("buy", "2024-02-10", xeqt, "4", "")
	sell := insert("sell", "2024-03-10", xeqt, "2.25", "")
	insert("split", "2024-04-01", xeqt, "", "2")
	insert("drip", "2024-05-01", xeqt, "0.125", "")
	bad := insert("buy", "2024-05-02", vab, "100", "")
	insert("buy", "2024-06-01", vab, "7", "")
	// Correction : la vente de 2,25 est annulée par une écriture inverse.
	rev := coredb.InsertTransactionParams{OwnerID: owner, AccountID: acc.ID, Type: "sell", TradeDate: day("2024-06-05"),
		InstrumentID: sell.InstrumentID, Quantity: sell.Quantity, Price: sell.Price, Currency: "CAD", Source: "manual", ReversesID: &sell.ID}
	if _, err := q.InsertTransaction(ctx, rev); err != nil {
		t.Fatalf("écriture inverse: %v", err)
	}
	revBad := rev
	revBad.ReversesID = &bad.ID
	revBad.InstrumentID = &vab
	revBad.Quantity = dec("99")
	if _, err := q.InsertTransaction(ctx, revBad); err == nil {
		t.Error("une inverse qui ne recopie pas l'originale doit être refusée")
	}
	// Transfert de titres vers le second compte.
	if _, err := q.InsertTransaction(ctx, coredb.InsertTransactionParams{OwnerID: owner, AccountID: acc.ID, Type: "transfer",
		TradeDate: day("2024-07-01"), InstrumentID: &vab, Quantity: dec("7"), CounterpartAccountID: &acc2.ID,
		Currency: "CAD", Source: "manual"}); err != nil {
		t.Fatal(err)
	}

	// (10,5 + 4) x 2 + 0,125 = 29,125 ; la vente corrigée n'a aucun effet ; VAB : 100 + 7 - 7 = 100.
	want := map[string]string{"XEQT.TO": "29.125", "VAB.TO": "100"}
	pos, err := q.ListPositionsAt(ctx, coredb.ListPositionsAtParams{OwnerID: owner, AccountID: acc.ID, AsOf: day("2024-12-31")})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != len(want) {
		t.Fatalf("%d positions, %d attendues: %+v", len(pos), len(want), pos)
	}
	for _, p := range pos {
		if !p.Quantity.Equal(decimal.RequireFromString(want[p.Symbol])) {
			t.Errorf("%s: %s, attendu %s", p.Symbol, p.Quantity, want[p.Symbol])
		}
	}
	// Au 2024-03-31, avant le fractionnement et la correction : 10,5 + 4 = 14,5 (la vente corrigée est ignorée).
	pos, err = q.ListPositionsAt(ctx, coredb.ListPositionsAtParams{OwnerID: owner, AccountID: acc.ID, AsOf: day("2024-03-31")})
	if err != nil || len(pos) != 1 || !pos[0].Quantity.Equal(decimal.RequireFromString("14.5")) {
		t.Errorf("positions au 2024-03-31: %+v, %v", pos, err)
	}
	pos, err = q.ListPositionsAt(ctx, coredb.ListPositionsAtParams{OwnerID: owner, AccountID: acc2.ID, AsOf: day("2024-12-31")})
	if err != nil || len(pos) != 1 || pos[0].Symbol != "VAB.TO" || !pos[0].Quantity.Equal(decimal.NewFromInt(7)) {
		t.Errorf("compte de contrepartie: %+v, %v", pos, err)
	}
	// Un autre propriétaire ne voit rien.
	pos, err = q.ListPositionsAt(ctx, coredb.ListPositionsAtParams{OwnerID: uuid.MustParse(ownerB), AccountID: acc.ID, AsOf: day("2024-12-31")})
	if err != nil || len(pos) != 0 {
		t.Errorf("autre propriétaire: %+v, %v", pos, err)
	}

	t.Run("vue core.position", func(t *testing.T) {
		var n int
		var total decimal.Decimal
		if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(quantity), 0) FROM core.position WHERE owner_id = $1 AND account_id = $2`,
			ownerA, acc.ID).Scan(&n, &total); err != nil {
			t.Fatal(err)
		}
		if n != 2 || !total.Equal(decimal.RequireFromString("129.125")) {
			t.Errorf("vue: %d lignes, total %s (2 et 129.125 attendus)", n, total)
		}
	})

	t.Run("registre en insertion seule", func(t *testing.T) {
		for _, c := range []struct{ db, name, sql, want string }{
			{"app", "UPDATE", `UPDATE core.transaction SET note = 'x'`, "permission denied"},
			{"app", "DELETE", `DELETE FROM core.transaction`, "permission denied"},
			{"app", "TRUNCATE", `TRUNCATE core.transaction`, "permission denied"},
			{"admin", "UPDATE", `UPDATE core.transaction SET note = 'x'`, "insertion seule"},
			{"admin", "DELETE", `DELETE FROM core.transaction`, "insertion seule"},
			{"admin", "TRUNCATE", `TRUNCATE core.transaction`, "insertion seule"},
		} {
			var err error
			if c.db == "app" {
				_, err = pool.Exec(ctx, c.sql)
			} else {
				_, err = admin.ExecContext(ctx, c.sql)
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s %s: %v, %q attendu", c.db, c.name, err, c.want)
			}
		}
	})

	t.Run("propriétaires isolés par clé composite", func(t *testing.T) {
		other := uuid.MustParse(ownerB)
		_, err := q.InsertTransaction(ctx, coredb.InsertTransactionParams{OwnerID: other, AccountID: acc.ID, Type: "contribution",
			TradeDate: day("2024-01-01"), Amount: dec("100"), Currency: "CAD", Source: "manual"})
		if err == nil {
			t.Error("un propriétaire ne peut pas écrire dans le compte d'un autre")
		}
	})

	t.Run("relevé unique par mois", func(t *testing.T) {
		p := coredb.CreateStatementParams{OwnerID: owner, AccountID: acc.ID, Month: "2024-01", EndValue: decimal.NewFromInt(1000)}
		if _, err := q.CreateStatement(ctx, p); err != nil {
			t.Fatal(err)
		}
		if _, err := q.CreateStatement(ctx, p); err == nil {
			t.Error("doublon de mois accepté")
		}
		p.Month = "2024-13"
		if _, err := q.CreateStatement(ctx, p); err == nil {
			t.Error("mois invalide accepté")
		}
	})

	t.Run("droits minimaux de finance_app", func(t *testing.T) {
		for _, s := range []string{
			`DELETE FROM core.account`, `DELETE FROM core.statement`, `DELETE FROM etf.etf_profile`,
			`DELETE FROM etf.model`, `UPDATE etf.model_version SET cash_target = 0`,
			`DELETE FROM etf.model_version`, `UPDATE etf.model_holding SET weight = 0`, `DELETE FROM etf.model_holding`,
			`TRUNCATE core.account`,
		} {
			if _, err := pool.Exec(ctx, s); err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("%s: %v, « permission denied » attendu", s, err)
			}
		}
		// Contrôle inverse : écriture autorisée là où prévu.
		if _, err := pool.Exec(ctx, `UPDATE core.account SET broker = 'Y' WHERE owner_id = $1`, ownerA); err != nil {
			t.Errorf("UPDATE account: %v", err)
		}
		var n int
		err := admin.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.role_table_grants
			WHERE grantee = 'finance_app' AND privilege_type IN ('TRUNCATE', 'REFERENCES', 'TRIGGER')
			  AND table_schema IN ('core', 'etf', 'stockeval')`).Scan(&n)
		if err != nil || n != 0 {
			t.Errorf("droits TRUNCATE/REFERENCES/TRIGGER: %d, %v", n, err)
		}
	})

	t.Run("modèles et profils", func(t *testing.T) {
		exec := func(sql string, args ...any) error {
			_, err := admin.ExecContext(ctx, sql, args...)
			return err
		}
		var model string
		if err := admin.QueryRowContext(ctx, `INSERT INTO etf.model (owner_id, name) VALUES ($1, 'M') RETURNING id`, ownerA).Scan(&model); err != nil {
			t.Fatal(err)
		}
		if err := exec(`INSERT INTO etf.model_version (owner_id, model_id, version, equity_ca, equity_us) VALUES ($1, $2, 1, 0.5, 0.4)`, ownerA, model); err == nil {
			t.Error("cibles ne totalisant pas 1 acceptées")
		}
		if err := exec(`INSERT INTO etf.model_version (owner_id, model_id, version, equity_ca, equity_us, cash_target) VALUES ($1, $2, 1, 0.5, 0.45, 0.05)`, ownerA, model); err != nil {
			t.Fatal(err)
		}
		if err := exec(`INSERT INTO etf.model_holding (owner_id, model_id, version, position, instrument_id, weight) VALUES ($1, $2, 1, 21, $3, 0.1)`, ownerA, model, xeqt); err == nil {
			t.Error("21e fonds accepté")
		}
		if err := exec(`INSERT INTO etf.etf_profile (owner_id, instrument_id, as_of, type, equity_ca, source) VALUES ($1, $2, '2025-10-31', 'equity', 0.9, 'manual')`, ownerA, xeqt); err == nil {
			t.Error("répartition ne totalisant pas 1 acceptée")
		}
		if err := exec(`INSERT INTO etf.etf_profile (owner_id, instrument_id, as_of, type, equity_ca, equity_us, mer, source) VALUES ($1, $2, '2025-10-31', 'equity', 0.3, 0.7, 0.002, 'manual')`, ownerA, xeqt); err != nil {
			t.Errorf("profil valide refusé: %v", err)
		}
	})
}
