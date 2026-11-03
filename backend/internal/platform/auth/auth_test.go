package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
)

const audience = "finance-api"

// idp est un fournisseur OIDC factice local (httptest) : aucun réseau externe.
type idp struct {
	srv *httptest.Server
	key *rsa.PrivateKey
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/finance/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.issuer(),
			"jwks_uri":                              p.issuer() + "/certs",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/realms/finance/certs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) issuer() string { return p.srv.URL + "/realms/finance" }

type claims struct {
	Issuer   string
	Audience string
	Subject  string
	Expiry   time.Time
}

func (p *idp) validClaims() claims {
	return claims{Issuer: p.issuer(), Audience: audience, Subject: "user-123", Expiry: time.Now().Add(5 * time.Minute)}
}

func sign(t *testing.T, alg jose.SignatureAlgorithm, key any, c claims) string {
	t.Helper()
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := jwt.Signed(s).Claims(jwt.Claims{
		Issuer: c.Issuer, Subject: c.Subject, Audience: jwt.Audience{c.Audience},
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), Expiry: jwt.NewNumericDate(c.Expiry),
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func newServer(t *testing.T, p *idp) http.Handler {
	t.Helper()
	v, err := auth.NewVerifier(auth.Config{Issuer: p.issuer(), Audience: audience}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /api/v1/stocks", func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.OwnerID(r.Context())
		_, _ = w.Write([]byte(id))
	})
	return v.Middleware("/healthz", "/readyz")(mux)
}

func get(h http.Handler, path, authz string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddleware(t *testing.T) {
	t.Parallel()
	p := newIDP(t)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&p.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	mut := func(f func(*claims)) claims { c := p.validClaims(); f(&c); return c }
	tests := []struct {
		name  string
		authz string
		want  int
		owner string
	}{
		{"sans jeton", "", 401, ""},
		{"schéma non Bearer", "Basic abc", 401, ""},
		{"jeton illisible", "Bearer n-importe-quoi", 401, ""},
		{"jeton valide", "Bearer " + sign(t, jose.RS256, p.key, p.validClaims()), 200, "user-123"},
		{"autre realm", "Bearer " + sign(t, jose.RS256, p.key, mut(func(c *claims) { c.Issuer = "http://evil/realms/autre" })), 401, ""},
		{"autre audience", "Bearer " + sign(t, jose.RS256, p.key, mut(func(c *claims) { c.Audience = "autre-api" })), 401, ""},
		{"expiré", "Bearer " + sign(t, jose.RS256, p.key, mut(func(c *claims) { c.Expiry = time.Now().Add(-time.Minute) })), 401, ""},
		{"signé par une autre clé", "Bearer " + sign(t, jose.RS256, other, p.validClaims()), 401, ""},
		{"HS256 avec la clé publique comme secret", "Bearer " + sign(t, jose.HS256, pubPEM, p.validClaims()), 401, ""},
		{"sub absent", "Bearer " + sign(t, jose.RS256, p.key, mut(func(c *claims) { c.Subject = "" })), 401, ""},
	}
	h := newServer(t, p)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := get(h, "/api/v1/stocks", tc.authz)
			if rec.Code != tc.want {
				t.Fatalf("code = %d, attendu %d (%s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want == 200 {
				if rec.Body.String() != tc.owner {
					t.Fatalf("owner_id = %q, attendu %q", rec.Body, tc.owner)
				}
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("Content-Type = %q", ct)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != float64(401) {
				t.Fatalf("corps problem+json invalide: %s", rec.Body)
			}
		})
	}
}

func TestHealthSansJeton(t *testing.T) {
	t.Parallel()
	h := newServer(t, newIDP(t))
	for _, path := range []string{"/healthz", "/readyz"} {
		if rec := get(h, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rec.Code)
		}
	}
}

func TestDecouverteIndisponible(t *testing.T) {
	t.Parallel()
	v, err := auth.NewVerifier(auth.Config{Issuer: "http://127.0.0.1:1/realms/finance", Audience: audience}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := v.Middleware("/healthz")(http.NotFoundHandler())
	if rec := get(h, "/api/v1/stocks", "Bearer x.y.z"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu 401 (fermé par défaut)", rec.Code)
	}
}

func TestNewVerifierConfigIncomplete(t *testing.T) {
	t.Parallel()
	if _, err := auth.NewVerifier(auth.Config{}, nil, nil); err == nil {
		t.Fatal("erreur attendue")
	}
}
