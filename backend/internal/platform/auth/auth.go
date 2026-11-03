// Package auth valide les jetons d'accès JWT émis par le realm Keycloak et
// pose l'identité du propriétaire (owner_id = sub, D1) dans le contexte.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Config décrit le realm attendu.
type Config struct {
	// Issuer est la valeur exacte de la revendication iss (URL du realm).
	Issuer string
	// DiscoveryURL, si renseignée, sert à lire la découverte OIDC et le JWKS
	// quand l'API joint Keycloak par une adresse interne différente de iss.
	DiscoveryURL string
	// Audience est la valeur attendue dans aud (ex. finance-api).
	Audience string
}

// Verifier valide les jetons. La découverte OIDC est faite à la première
// requête (puis réessayée tant qu'elle échoue) : l'API démarre sans Keycloak.
// Les clés JWKS sont mises en cache par go-oidc et rechargées quand un jeton
// porte un kid inconnu.
type Verifier struct {
	cfg    Config
	client *http.Client
	logger *slog.Logger

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewVerifier valide la configuration et construit le vérificateur.
// client et logger peuvent être nil.
func NewVerifier(cfg Config, client *http.Client, logger *slog.Logger) (*Verifier, error) {
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, errors.New("configurer l'authentification: issuer et audience sont obligatoires")
	}
	if client == nil {
		client = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Verifier{cfg: cfg, client: client, logger: logger}, nil
}

func (v *Verifier) get(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	ctx = oidc.ClientContext(ctx, v.client)
	url := v.cfg.Issuer
	if v.cfg.DiscoveryURL != "" {
		url = v.cfg.DiscoveryURL
		ctx = oidc.InsecureIssuerURLContext(ctx, v.cfg.Issuer)
	}
	provider, err := oidc.NewProvider(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("découvrir le realm %s: %w", url, err)
	}
	// Le contexte du vérificateur vit autant que le processus : il sert aux
	// rechargements du JWKS, pas à la requête qui a déclenché la découverte.
	v.verifier = provider.VerifierContext(oidc.ClientContext(context.Background(), v.client), &oidc.Config{
		ClientID:             v.cfg.Audience, // contrôle aud
		SupportedSigningAlgs: []string{oidc.RS256},
	})
	return v.verifier, nil
}

// Verify contrôle signature (RS256 seulement), iss, aud et exp, et renvoie le sub.
func (v *Verifier) Verify(ctx context.Context, raw string) (string, error) {
	ver, err := v.get(ctx)
	if err != nil {
		return "", err
	}
	tok, err := ver.Verify(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("vérifier le jeton: %w", err)
	}
	if tok.Subject == "" {
		return "", errors.New("vérifier le jeton: sub absent")
	}
	return tok.Subject, nil
}

type ownerKey struct{}

// WithOwner pose l'identifiant du propriétaire dans le contexte.
func WithOwner(ctx context.Context, ownerID string) context.Context {
	return context.WithValue(ctx, ownerKey{}, ownerID)
}

// OwnerID renvoie le propriétaire authentifié de la requête.
func OwnerID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ownerKey{}).(string)
	return id, ok && id != ""
}

// Middleware exige un jeton Bearer valide, sauf pour les chemins publics
// (exacts, ex. /healthz et /readyz). Les refus sont des 401 problem+json
// sans détail sur la cause, qui reste dans le journal.
func (v *Verifier) Middleware(public ...string) func(http.Handler) http.Handler {
	skip := make(map[string]struct{}, len(public))
	for _, p := range public {
		skip[p] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			raw, ok := bearer(r)
			if !ok {
				unauthorized(w, r)
				return
			}
			sub, err := v.Verify(r.Context(), raw)
			if err != nil {
				v.logger.WarnContext(r.Context(), "jeton refusé", slog.String("erreur", err.Error()))
				unauthorized(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithOwner(r.Context(), sub)))
		})
	}
}

func bearer(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="finance"`)
	httpx.WriteProblem(w, httpx.Problem{
		Type:     "https://finance-privee.local/problems/unauthorized",
		Title:    "Authentification requise",
		Status:   http.StatusUnauthorized,
		Detail:   "Jeton d'accès absent ou invalide.",
		Instance: r.URL.Path,
	})
}
