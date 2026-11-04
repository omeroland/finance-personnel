package observability

import (
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

const masked = "[masqué]"

// Fragments de clé dont la valeur n'est jamais journalisée (jetons, secrets,
// en-têtes d'authentification, montants de portefeuille).
var sensitiveKeys = []string{
	"authorization", "token", "jeton", "password", "mot_de_passe", "passwd",
	"secret", "cookie", "apikey", "api_key",
	"amount", "montant", "balance", "solde", "portfolio", "portefeuille",
}

var (
	bearerValue = regexp.MustCompile(`(?i)\bbearer\s+\S+`)
	jwtValue    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
)

// NewLogger renvoie un journal slog JSON dont tout attribut sensible est
// masqué par ReplaceAttr : filet de sécurité en plus de la discipline des
// appelants (les intergiciels ne journalisent déjà aucun en-tête).
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: redact}))
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey || a.Key == slog.LevelKey || a.Key == slog.MessageKey {
		return a
	}
	k := strings.ToLower(a.Key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return slog.String(a.Key, masked)
		}
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(scrub(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			a.Value = slog.StringValue(scrub(err.Error()))
		} else if raw, mErr := json.Marshal(a.Value.Any()); mErr == nil {
			// Structure quelconque : si sa forme JSON contient un jeton, on la remplace par la version épurée.
			if clean := scrub(string(raw)); clean != string(raw) {
				a.Value = slog.StringValue(clean)
			}
		}
	default:
	}
	return a
}

// scrub masque les valeurs qui ressemblent à un jeton dans un texte libre
// (message d'erreur d'une bibliothèque, par exemple).
func scrub(s string) string {
	s = bearerValue.ReplaceAllString(s, "Bearer "+masked)
	return jwtValue.ReplaceAllString(s, masked)
}
