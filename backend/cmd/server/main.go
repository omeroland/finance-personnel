// Commande server : point d'entrée HTTP de l'API Finance privée.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/stockeval/http"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

const (
	defaultAddr     = ":8080"
	shutdownTimeout = 15 * time.Second
	// requestTimeout borne chaque requête (l'import xlsx continue en arrière-plan, hors de ce délai).
	requestTimeout = 25 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config{addr: os.Getenv("HTTP_ADDR"), databaseURL: os.Getenv("DATABASE_URL"), auth: authConfig()}
	if err := run(logger, cfg); err != nil {
		logger.Error("arrêt sur erreur", slog.String("erreur", err.Error()))
		os.Exit(1)
	}
}

type config struct {
	addr string
	// databaseURL est la chaîne de connexion du rôle finance_app (D41) ; obligatoire.
	databaseURL string
	auth        auth.Config
}

// authConfig lit le realm attendu dans l'environnement. OIDC_ISSUER est
// obligatoire (l'API refuse de démarrer sans) ; OIDC_DISCOVERY_URL sert quand
// Keycloak est joint par une adresse interne différente de l'issuer public.
func authConfig() auth.Config {
	aud := os.Getenv("OIDC_AUDIENCE")
	if aud == "" {
		aud = "finance-api"
	}
	return auth.Config{
		Issuer:       os.Getenv("OIDC_ISSUER"),
		DiscoveryURL: os.Getenv("OIDC_DISCOVERY_URL"),
		Audience:     aud,
	}
}

func run(logger *slog.Logger, cfg config) error {
	verifier, err := auth.NewVerifier(cfg.auth, nil, logger)
	if err != nil {
		return fmt.Errorf("authentification (OIDC_ISSUER requis): %w", err)
	}
	if cfg.databaseURL == "" {
		return errors.New("base de données (DATABASE_URL requis)")
	}
	rules, err := engine.DefaultRules()
	if err != nil {
		return fmt.Errorf("charger les règles: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// pgxpool.New ne se connecte pas : l'API démarre sans base, /readyz le signale.
	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("configurer le pool PostgreSQL: %w", err)
	}
	defer pool.Close()

	st := store.New(pool)
	api, err := httpapi.New(httpapi.Config{Repos: st, Importer: ingest.NewImporter(st), Rules: rules, Logger: logger})
	if err != nil {
		return fmt.Errorf("assembler les handlers: %w", err)
	}

	addr := cfg.addr
	if addr == "" {
		addr = defaultAddr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler(logger, verifier, api, pool),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("serveur démarré", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("écouter sur %s: %w", addr, err)
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("signal reçu, arrêt en cours")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("arrêt propre du serveur: %w", err)
	}
	if err := api.Close(shutdownCtx); err != nil {
		return err
	}
	logger.Info("serveur arrêté")
	return nil
}

// pinger vérifie la disponibilité de la base (satisfait par *pgxpool.Pool).
type pinger interface {
	Ping(ctx context.Context) error
}

// handler assemble la pile HTTP : identifiant de requête, journal d'accès,
// récupération de panique, JWT (sauf /healthz et /readyz), limite de corps, délai.
func handler(logger *slog.Logger, verifier *auth.Verifier, api *httpapi.Handler, db pinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", ok)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			logger.WarnContext(r.Context(), "base injoignable", slog.String("erreur", err.Error()))
			p, _ := httpx.ProblemOf(httpx.Unavailable(err), r.URL.Path)
			httpx.WriteProblem(w, p)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/api/", api.Routes())

	return httpx.Chain(mux,
		httpx.RequestID,
		httpx.AccessLog(logger),
		httpx.Recover(logger),
		verifier.Middleware("/healthz", "/readyz"),
		httpapi.BodyLimit(),
		httpx.Timeout(requestTimeout),
	)
}

func ok(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
