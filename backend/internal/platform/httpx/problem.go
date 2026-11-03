// Package httpx regroupe les briques HTTP communes : erreurs RFC 9457 et
// intergiciels (identifiant de requête, journal d'accès, récupération de
// panique, limite de corps, délai).
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// ProblemContentType est le type de média des erreurs de l'API (RFC 9457).
const ProblemContentType = "application/problem+json"

// problemBase préfixe les identifiants de famille d'erreur.
const problemBase = "https://finance-privee.local/problems/"

// FieldError est une erreur de validation sur un champ (schéma FieldError du contrat).
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problem est le corps d'une erreur (schéma Problem du contrat). Aucun champ
// ne doit porter de trace interne.
type Problem struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// WriteProblem écrit p en application/problem+json avec le code p.Status.
func WriteProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", ProblemContentType)
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Warn("écrire l'erreur problem+json", slog.String("erreur", err.Error()))
	}
}

// Error est une erreur d'API : elle se traduit en problem+json. Cause, si
// présente, n'est jamais écrite dans la réponse : elle sert au journal.
type Error struct {
	Status int
	Kind   string
	Title  string
	Detail string
	Fields []FieldError
	Cause  error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Title + ": " + e.Cause.Error()
	}
	return e.Title + ": " + e.Detail
}

// Unwrap expose la cause pour errors.Is et errors.As.
func (e *Error) Unwrap() error { return e.Cause }

func newError(status int, kind, title, detail string) *Error {
	return &Error{Status: status, Kind: kind, Title: title, Detail: detail}
}

// BadRequest : requête invalide, avec les erreurs champ par champ.
func BadRequest(detail string, fields ...FieldError) *Error {
	e := newError(http.StatusBadRequest, "validation", "Requête invalide", detail)
	e.Fields = fields
	return e
}

// Unauthorized : authentification absente ou invalide.
func Unauthorized(detail string) *Error {
	return newError(http.StatusUnauthorized, "unauthorized", "Authentification requise", detail)
}

// NotFound : ressource introuvable.
func NotFound(detail string) *Error {
	return newError(http.StatusNotFound, "not-found", "Ressource introuvable", detail)
}

// Conflict : la ressource existe déjà.
func Conflict(detail string) *Error {
	return newError(http.StatusConflict, "conflict", "Ressource existante", detail)
}

// PayloadTooLarge : corps de requête trop volumineux.
func PayloadTooLarge(detail string) *Error {
	return newError(http.StatusRequestEntityTooLarge, "payload-too-large", "Corps trop volumineux", detail)
}

// NotImplemented : fonction prévue pour une phase ultérieure.
func NotImplemented(detail string) *Error {
	return newError(http.StatusNotImplemented, "not-implemented", "Non implémenté", detail)
}

// Internal : erreur interne ; le détail reste générique, la cause va au journal.
func Internal(cause error) *Error {
	e := newError(http.StatusInternalServerError, "internal", "Erreur interne", "Une erreur interne est survenue.")
	e.Cause = cause
	return e
}

// Unavailable : service momentanément indisponible (délai dépassé, base injoignable).
func Unavailable(cause error) *Error {
	e := newError(http.StatusServiceUnavailable, "unavailable", "Service indisponible", "Le service est momentanément indisponible.")
	e.Cause = cause
	return e
}

// ProblemOf convertit une erreur quelconque en Problem sans fuite interne :
// une erreur inconnue devient un 500 générique. Le second résultat est la
// cause à journaliser (nil pour une erreur client).
func ProblemOf(err error, instance string) (Problem, error) {
	var ae *Error
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &ae):
	case errors.As(err, &mbe):
		ae = PayloadTooLarge("Le corps de la requête dépasse la taille permise.")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		ae = Unavailable(err)
	default:
		ae = Internal(err)
	}
	p := Problem{
		Type: problemBase + ae.Kind, Title: ae.Title, Status: ae.Status,
		Detail: ae.Detail, Instance: instance, Errors: ae.Fields,
	}
	if ae.Status >= http.StatusInternalServerError {
		cause := ae.Cause
		if cause == nil {
			cause = err
		}
		return p, cause
	}
	return p, nil
}

// ErrorHandler renvoie un gestionnaire d'erreurs pour le serveur strict
// d'oapi-codegen : écrit un problem+json et journalise la cause des 5xx.
func ErrorHandler(logger *slog.Logger) func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p, cause := ProblemOf(err, r.URL.Path)
		if cause != nil {
			logger.ErrorContext(r.Context(), "erreur interne",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("erreur", cause.Error()))
		}
		WriteProblem(w, p)
	}
}
