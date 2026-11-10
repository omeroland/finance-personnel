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

	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	etfhttp "github.com/omeroland/finance-privee/backend/internal/modules/etf/http"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/stockeval/http"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
	"github.com/omeroland/finance-privee/backend/internal/platform/observability"
)

const (
	defaultAddr     = ":8080"
	shutdownTimeout = 15 * time.Second
	// requestTimeout borne chaque requête (l'import xlsx continue en arrière-plan, hors de ce délai).
	requestTimeout = 25 * time.Second
)

func main() {
	logger := observability.NewLogger(os.Stdout, slog.LevelInfo)
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(context.Background(), logger, migrateConfigFromEnv(os.Getenv)); err != nil {
			logger.Error("échec des migrations", slog.String("erreur", err.Error()))
			os.Exit(1)
		}
		return
	}
	cfg := config{addr: os.Getenv("HTTP_ADDR"), metricsAddr: os.Getenv("METRICS_ADDR"), databaseURL: os.Getenv("DATABASE_URL"), auth: authConfig()}
	if err := run(logger, cfg); err != nil {
		logger.Error("arrêt sur erreur", slog.String("erreur", err.Error()))
		os.Exit(1)
	}
}

type config struct {
	addr string
	// metricsAddr est l'adresse interne des métriques (défaut :9090), jamais publiée.
	metricsAddr string
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

	metrics := observability.NewMetrics()
	st := store.New(pool)
	api, err := httpapi.New(httpapi.Config{Repos: st, Importer: ingest.NewImporter(st), Rules: rules, Logger: logger, Observer: metrics})
	if err != nil {
		return fmt.Errorf("assembler les handlers: %w", err)
	}

	core := corestore.New(pool)
	etfStore := etfstore.New(core)
	etfAPI, err := etfhttp.New(etfhttp.Config{Ledger: core, Catalog: etfStore, Portfolio: etfStore, Imports: etfStore, Logger: logger})
	if err != nil {
		return fmt.Errorf("assembler les handlers du module 2: %w", err)
	}

	addr := cfg.addr
	if addr == "" {
		addr = defaultAddr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler(logger, verifier, routes(api, etfAPI), metrics, pool),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	metricsSrv := observability.NewMetricsServer(cfg.metricsAddr, metrics)
	errCh := make(chan error, 2)
	go func() {
		logger.Info("métriques sur le port interne", slog.String("addr", metricsSrv.Addr))
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("écouter les métriques sur %s: %w", metricsSrv.Addr, err)
		}
	}()
	go func() {
		logger.Info("serveur démarré", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("écouter sur %s: %w", addr, err)
		}
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
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("arrêt du serveur de métriques: %w", err)
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

// handler assemble la pile HTTP : identifiant de requête, métriques, journal
// d'accès, récupération de panique, JWT (sauf /healthz et /readyz), limite de
// corps, délai. /readyz exige la base et la découverte OIDC (JWKS en cache).
func handler(logger *slog.Logger, verifier *auth.Verifier, routes http.Handler, metrics *observability.Metrics, db pinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", observability.Healthz)
	mux.HandleFunc("GET /readyz", observability.Readyz(logger, db.Ping, verifier.Ready))
	mux.Handle("/api/", routes)

	return httpx.Chain(mux,
		httpx.RequestID,
		metrics.Middleware,
		httpx.AccessLog(logger),
		httpx.Recover(logger),
		verifier.Middleware("/healthz", "/readyz"),
		bodyLimit(),
		httpx.Timeout(requestTimeout),
	)
}

// composed assemble le contrat complet : le module 1 (et les routes encore
// provisoires) vient de stockeval, le registre et le catalogue de etf/http.
// Aucune méthode n'existe des deux côtés (D401).
type (
	module1 = httpapi.Handler
	module2 = etfhttp.Handler
)

type composed struct {
	*module1
	*module2
}

// routes monte l'API complète derrière le JWT.
func routes(api *httpapi.Handler, etf *etfhttp.Handler) http.Handler {
	return api.RoutesFor(composed{module1: api, module2: etf})
}

// bodyLimit borne le corps des requêtes : l'import CSV (chemin à identifiant
// variable) reçoit la limite de 10 Mo, le reste celle de l'API.
func bodyLimit() httpx.Middleware {
	def := httpapi.BodyLimit()
	csv := httpx.BodyLimit(etfhttp.CSVBodyLimit, nil)
	return func(next http.Handler) http.Handler {
		d, c := def(next), csv(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && etfhttp.CSVPath.MatchString(r.URL.Path) {
				c.ServeHTTP(w, r)
				return
			}
			d.ServeHTTP(w, r)
		})
	}
}
