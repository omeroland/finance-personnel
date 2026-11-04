package observability

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Check vérifie une dépendance ; une erreur la déclare indisponible.
type Check func(ctx context.Context) error

const readyTimeout = 3 * time.Second

// Healthz répond 200 tant que le processus tourne.
func Healthz(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// Readyz répond 200 si tous les contrôles passent, sinon 503 problem+json
// sans détail interne (la cause va au journal).
func Readyz(logger *slog.Logger, checks ...Check) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		for _, c := range checks {
			if err := c(ctx); err != nil {
				logger.WarnContext(r.Context(), "dépendance indisponible",
					slog.String("request_id", httpx.RequestIDFrom(r.Context())),
					slog.String("erreur", err.Error()))
				p, _ := httpx.ProblemOf(httpx.Unavailable(err), r.URL.Path)
				httpx.WriteProblem(w, p)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}
