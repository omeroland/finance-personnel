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

	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
)

const (
	defaultAddr     = ":8080"
	shutdownTimeout = 15 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger, os.Getenv("HTTP_ADDR"), authConfig()); err != nil {
		logger.Error("arrêt sur erreur", slog.String("erreur", err.Error()))
		os.Exit(1)
	}
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

func run(logger *slog.Logger, addr string, authCfg auth.Config) error {
	verifier, err := auth.NewVerifier(authCfg, nil, logger)
	if err != nil {
		return fmt.Errorf("authentification (OIDC_ISSUER requis): %w", err)
	}

	if addr == "" {
		addr = defaultAddr
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", ok)
	mux.HandleFunc("GET /readyz", ok)

	srv := &http.Server{
		Addr:              addr,
		Handler:           verifier.Middleware("/healthz", "/readyz")(mux),
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
	logger.Info("serveur arrêté")
	return nil
}

func ok(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
