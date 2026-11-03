// Package httpx regroupe les briques HTTP communes : erreurs RFC 9457.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ProblemContentType est le type de média des erreurs de l'API (RFC 9457).
const ProblemContentType = "application/problem+json"

// Problem est le corps d'une erreur (schéma Problem du contrat). Aucun champ
// ne doit porter de trace interne.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// WriteProblem écrit p en application/problem+json avec le code p.Status.
func WriteProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", ProblemContentType)
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Warn("écrire l'erreur problem+json", slog.String("erreur", err.Error()))
	}
}
