package httpapi_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/etf/http"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
)

// fakeTxRepo dédoublonne par clé et rang, comme le vrai dépôt.
type fakeTxRepo struct {
	mu   sync.Mutex
	seen map[string]int
	got  []string // comptes reçus
}

func (f *fakeTxRepo) SaveTransactions(_ context.Context, owner uuid.UUID, txs []ingest.Transaction) ([]ingest.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen == nil {
		f.seen = map[string]int{}
	}
	out := make([]ingest.Outcome, len(txs))
	for i, t := range txs {
		f.got = append(f.got, t.Account)
		k := owner.String() + "|" + t.NaturalKey
		if f.seen[k] >= t.Occurrence {
			out[i] = ingest.Outcome{Status: ingest.OutcomeDuplicate}
			continue
		}
		f.seen[k]++
		out[i] = ingest.Outcome{Status: ingest.OutcomeCreated}
	}
	return out, nil
}

func multipartBody(t testing.TB, field string, data []byte) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if field != "" {
		fw, err := w.CreateFormFile(field, "releve.csv")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	_ = w.Close()
	return w.FormDataContentType(), buf.Bytes()
}

func (a *api) importCSV(acc uuid.UUID, broker string, data []byte) resp {
	a.t.Helper()
	ct, body := multipartBody(a.t, "file", data)
	return a.doType("POST", base+"/accounts/"+acc.String()+"/imports/csv?broker="+broker, ct, body)
}

const csvOK = `date,compte,type,symbole,quantité,prix,devise,montant
2026-01-05,CELI,contribution,,,,CAD,1000
2026-01-10,CELI,buy,XEQT,10,30.5,CAD,
2026-01-11,CELI,buy,XEQT,0,30.5,CAD,
2026-01-12,REER,buy,XEQT,1,30.5,CAD,
`

func TestImportCSV(t *testing.T) {
	t.Parallel()
	repo := &fakeTxRepo{}
	h, err := httpapi.New(httpapi.Config{Ledger: newFakeLedger(), Catalog: fakeCatalog{}, Portfolio: newFakePortfolio(), Imports: repo, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := newAPI(t, h, uuid.New(), &bytes.Buffer{}), newAPI(t, h, uuid.New(), &bytes.Buffer{})
	acc := alice.account("CELI")

	var rep gen.CsvImportReport
	r := alice.importCSV(acc.Id, "generic", []byte(csvOK))
	if r.Status != 200 {
		t.Fatalf("import: %d %s", r.Status, r.Body)
	}
	r.json(t, &rep)
	if rep.RowsRead != 4 || rep.Created != 2 || rep.Invalid != 1 || rep.Rejected != 1 || len(rep.Entries) < 2 {
		t.Fatalf("rapport: %+v", rep)
	}
	for _, e := range rep.Entries {
		if e.Line < 2 || e.Message == "" {
			t.Fatalf("entrée: %+v", e)
		}
	}

	alice.importCSV(acc.Id, "generic", []byte(csvOK)).json(t, &rep)
	if rep.Created != 0 || rep.Duplicates != 2 {
		t.Fatalf("ré-import: %+v", rep)
	}

	n := len(repo.got)
	if r := bob.importCSV(acc.Id, "generic", []byte(csvOK)); r.Status != 404 {
		t.Fatalf("autre propriétaire: %d %s", r.Status, r.Body)
	}
	if len(repo.got) != n {
		t.Fatal("le dépôt a reçu des lignes d'un autre propriétaire")
	}
}

func TestImportCSVBadInput(t *testing.T) {
	t.Parallel()
	h, err := httpapi.New(httpapi.Config{Ledger: newFakeLedger(), Catalog: fakeCatalog{}, Portfolio: newFakePortfolio(), Imports: &fakeTxRepo{}, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	alice := newAPI(t, h, uuid.New(), &bytes.Buffer{})
	acc := alice.account("CELI")

	cases := map[string]struct {
		broker string
		data   []byte
		field  string
		want   int
	}{
		"vide":             {"generic", nil, "file", 400},
		"sans en-têtes":    {"generic", []byte("a,b,c\n1,2,3\n"), "file", 400},
		"binaire":          {"generic", []byte{0, 1, 2, 0xff}, "file", 400},
		"courtier inconnu": {"autre", []byte(csvOK), "file", 400},
		"champ absent":     {"generic", []byte(csvOK), "", 400},
		"trop gros":        {"generic", []byte(strings.Repeat("x", httpapi.MaxCSVSize+1)), "file", 413},
	}
	for name, c := range cases {
		ct, body := multipartBody(t, c.field, c.data)
		r := alice.doType("POST", base+"/accounts/"+acc.Id.String()+"/imports/csv?broker="+c.broker, ct, body)
		if r.Status != c.want {
			t.Errorf("%s: %d (attendu %d) %s", name, r.Status, c.want, r.Body)
		}
	}
	if r := alice.doRaw("POST", base+"/accounts/"+acc.Id.String()+"/imports/csv?broker=generic", []byte("x")); r.Status != 400 {
		t.Errorf("non multipart: %d %s", r.Status, r.Body)
	}
}
