package httpapi_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
)

type recObserver struct {
	mu          sync.Mutex
	evaluations int
	imports     []string
}

func (o *recObserver) EvaluationDone(time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evaluations++
}

func (o *recObserver) ImportFinished(status string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.imports = append(o.imports, status)
}

// Le handler signale chaque évaluation et l'issue de chaque import à l'observateur.
func TestObserverHooks(t *testing.T) {
	t.Parallel()
	obs := &recObserver{}
	imp := &fakeImporter{}
	a := newAPIWith(t, newFakeRepos(), imp, uuid.New(), obs)
	a.addStock(sym)
	a.fillPerfect(sym)
	if r := a.doJSON("GET", base+"/stocks/"+sym+"/evaluation", nil); r.Status != 200 {
		t.Fatalf("évaluation: %d %s", r.Status, r.Body)
	}

	body, ct := multipartBody(t, "2006", []byte("xlsx"))
	a.waitJob(a.importJobID(t, ct, body))
	imp.err = errors.New("boum")
	a.waitJob(a.importJobID(t, ct, body))

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if obs.evaluations < 1 {
		t.Fatal("aucune durée d'évaluation signalée")
	}
	if len(obs.imports) != 2 || obs.imports[0] != "succeeded" || obs.imports[1] != "failed" {
		t.Fatalf("issues d'import: %v", obs.imports)
	}
}

func (a *api) importJobID(t *testing.T, ct string, body []byte) string {
	t.Helper()
	r := a.do("POST", base+"/imports/xlsx", ct, body)
	var job gen.Job
	r.json(t, &job)
	if r.Status != 202 {
		t.Fatalf("import: %d %s", r.Status, r.Body)
	}
	return job.Id.String()
}
