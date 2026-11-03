//go:build integration

package auth_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
)

// Image épinglée par digest (Keycloak 26.4).
const keycloakImage = "quay.io/keycloak/keycloak@sha256:9409c59bdfb65dbffa20b11e6f18b8abb9281d480c7ca402f51ed3d5977e6007"

func startKeycloak(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        keycloakImage,
			ExposedPorts: []string{"8080/tcp"},
			Cmd:          []string{"start-dev", "--import-realm"},
			Files: []testcontainers.ContainerFile{{
				HostFilePath:      "../../../../deploy/keycloak/realm-finance-test.json",
				ContainerFilePath: "/opt/keycloak/data/import/realm-finance-test.json",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForHTTP("/realms/finance/.well-known/openid-configuration").
				WithPort("8080/tcp").WithStartupTimeout(3 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("démarrer Keycloak (Docker requis): %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := c.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("http://%s:%s/realms/finance", host, port.Port())
}

func directGrant(t *testing.T, issuer string) string {
	t.Helper()
	resp, err := http.PostForm(issuer+"/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {"finance-test"},
		"username": {"test-user"}, "password": {"FAUX-mot-de-passe-de-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.AccessToken == "" {
		t.Fatalf("jeton non obtenu (HTTP %d): %v", resp.StatusCode, err)
	}
	return body.AccessToken
}

func TestKeycloakJeton(t *testing.T) {
	issuer := startKeycloak(t)
	v, err := auth.NewVerifier(auth.Config{Issuer: issuer, Audience: "finance-api"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var gotOwner string
	h := v.Middleware("/healthz")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOwner, _ = auth.OwnerID(r.Context())
	}))
	call := func(authz string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := call(""); code != 401 {
		t.Fatalf("sans jeton: %d", code)
	}

	tok := directGrant(t, issuer)
	if code := call("Bearer " + tok); code != 200 {
		t.Fatalf("jeton valide: %d", code)
	}
	if gotOwner != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("owner_id = %q, attendu le sub de l'utilisateur de test", gotOwner)
	}

	// Attaque de confusion d'algorithme : HS256, clé publique du realm comme secret.
	resp, err := http.Get(issuer + "/protocol/openid-connect/certs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var jwks jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		t.Fatal(err)
	}
	for _, k := range jwks.Keys {
		if k.Use != "sig" || !strings.HasPrefix(k.Algorithm, "RS") {
			continue
		}
		der, err := x509.MarshalPKIXPublicKey(k.Key)
		if err != nil {
			t.Fatal(err)
		}
		secret := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
		forged := sign(t, jose.HS256, secret, claims{
			Issuer: issuer, Audience: "finance-api", Subject: "pirate", Expiry: time.Now().Add(time.Minute),
		})
		if code := call("Bearer " + forged); code != 401 {
			t.Fatalf("HS256 forgé: %d", code)
		}
	}
}
