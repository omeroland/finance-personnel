package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"time"
)

// Middleware enveloppe un gestionnaire.
type Middleware func(http.Handler) http.Handler

// Chain applique les intergiciels : le premier est le plus externe.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type requestIDKey struct{}

// RequestIDHeader est l'en-tête qui porte l'identifiant de requête.
const RequestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestIDFrom renvoie l'identifiant de requête du contexte ("" s'il manque).
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// RequestID reprend l'identifiant entrant s'il est sûr, sinon en génère un, puis le renvoie en en-tête.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			var b [12]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap permet à http.ResponseController d'atteindre l'écrivain d'origine.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog journalise chaque requête en JSON : méthode, chemin (sans requête
// ni en-têtes, donc sans jeton), code, durée, octets et identifiant.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logger.InfoContext(r.Context(), "requête",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// Recover transforme une panique en 500 problem+json sans trace pour le
// client ; la trace va au journal.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinelle de la bibliothèque standard
					panic(v)
				}
				logger.ErrorContext(r.Context(), "panique récupérée",
					slog.String("request_id", RequestIDFrom(r.Context())),
					slog.Any("panique", v),
					slog.String("pile", string(debug.Stack())))
				p, _ := ProblemOf(Internal(nil), r.URL.Path)
				WriteProblem(w, p)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit borne le corps des requêtes : limit octets, ou l'entrée de
// overrides pour le chemin exact. Un Content-Length trop grand est refusé
// tout de suite (413) ; sinon la lecture échoue au dépassement.
func BodyLimit(limit int64, overrides map[string]int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lim := limit
			if o, ok := overrides[r.URL.Path]; ok {
				lim = o
			}
			if r.ContentLength > lim {
				p, _ := ProblemOf(PayloadTooLarge("Le corps de la requête dépasse "+strconv.FormatInt(lim, 10)+" octets."), r.URL.Path)
				WriteProblem(w, p)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, lim)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout borne la durée du traitement par le contexte de la requête ;
// les dépôts et le moteur l'honorent.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
