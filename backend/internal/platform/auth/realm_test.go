package auth_test

import (
	"encoding/json"
	"os"
	"testing"
)

type realmFile struct {
	Realm               string        `json:"realm"`
	AccessTokenLifespan int           `json:"accessTokenLifespan"`
	Clients             []realmClient `json:"clients"`
	RegistrationAllowed bool          `json:"registrationAllowed"`
	BruteForceProtected bool          `json:"bruteForceProtected"`
}

type realmClient struct {
	ClientID        string            `json:"clientId"`
	PublicClient    bool              `json:"publicClient"`
	StandardFlow    bool              `json:"standardFlowEnabled"`
	Implicit        bool              `json:"implicitFlowEnabled"`
	DirectGrants    bool              `json:"directAccessGrantsEnabled"`
	ServiceAccounts bool              `json:"serviceAccountsEnabled"`
	Attributes      map[string]string `json:"attributes"`
	ProtocolMappers []realmMapper     `json:"protocolMappers"`
	RedirectUris    []string          `json:"redirectUris"`
	Secret          string            `json:"secret"`
}

type realmMapper struct {
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config"`
}

func loadRealm(t *testing.T, name string) (realmFile, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("../../../../deploy/keycloak/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var r realmFile
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	return r, generic
}

func audienceMapper(c realmClient) bool {
	for _, m := range c.ProtocolMappers {
		if m.ProtocolMapper == "oidc-audience-mapper" &&
			m.Config["included.custom.audience"] == "finance-api" && m.Config["access.token.claim"] == "true" {
			return true
		}
	}
	return false
}

func TestRealmProduction(t *testing.T) {
	t.Parallel()
	r, generic := loadRealm(t, "realm-finance.json")
	if r.Realm != "finance" || r.AccessTokenLifespan != 300 || r.RegistrationAllowed || !r.BruteForceProtected {
		t.Fatalf("réglages du realm inattendus: %+v", r)
	}
	if generic["sslRequired"] == "none" {
		t.Fatal("sslRequired ne doit pas être none en production")
	}
	if _, ok := generic["users"]; ok {
		t.Fatal("aucun utilisateur ne doit être embarqué dans le realm de production")
	}
	if len(r.Clients) != 1 || r.Clients[0].ClientID != "finance-web" {
		t.Fatalf("clients inattendus: %+v", r.Clients)
	}
	c := r.Clients[0]
	switch {
	case !c.PublicClient, !c.StandardFlow:
		t.Fatal("finance-web doit être public, en code d'autorisation")
	case c.DirectGrants:
		t.Fatal("l'obtention directe de jetons (password grant) est interdite en production")
	case c.Implicit, c.ServiceAccounts:
		t.Fatal("flux implicite et compte de service interdits")
	case c.Attributes["pkce.code.challenge.method"] != "S256":
		t.Fatal("PKCE S256 requis")
	case !audienceMapper(c):
		t.Fatal("mapper d'audience finance-api absent")
	case c.Secret != "":
		t.Fatal("aucun secret dans un client public")
	}
	for _, u := range c.RedirectUris {
		if u == "*" {
			t.Fatal("URI de redirection trop large")
		}
	}
}

func TestRealmTestAutoriseObtentionDirecte(t *testing.T) {
	t.Parallel()
	r, generic := loadRealm(t, "realm-finance-test.json")
	if r.Realm != "finance" {
		t.Fatalf("realm = %q", r.Realm)
	}
	found := false
	for _, c := range r.Clients {
		if c.ClientID == "finance-test" {
			found = c.DirectGrants && audienceMapper(c)
		}
	}
	if !found {
		t.Fatal("client finance-test avec obtention directe et audience attendu")
	}
	if _, ok := generic["users"]; !ok {
		t.Fatal("utilisateur de test attendu")
	}
}
