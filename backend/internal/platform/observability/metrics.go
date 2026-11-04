// Package observability regroupe journaux, métriques Prometheus et sondes de
// santé de l'API. Aucune logique métier : les modules appellent les hooks.
package observability

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Statuts d'un job d'import comptés par import_jobs_total.
const (
	ImportSucceeded = "succeeded"
	ImportFailed    = "failed"
)

// Metrics porte le registre et les quatre métriques de l'API. Le registre est
// propre à l'instance (pas d'état global) : un test crée le sien.
type Metrics struct {
	reg        *prometheus.Registry
	requests   *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	evaluation prometheus.Histogram
	imports    *prometheus.CounterVec
}

// NewMetrics crée le registre, y enregistre les métriques et les collecteurs
// du processus et du runtime Go.
func NewMetrics() *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Requêtes HTTP traitées, par méthode, route et classe de code.",
		}, []string{"method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Durée des requêtes HTTP, par méthode et route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		evaluation: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "stockeval_evaluation_duration_seconds",
			Help:    "Durée d'un appel au moteur d'évaluation.",
			Buckets: []float64{.0001, .0005, .001, .005, .01, .05, .1, .5, 1},
		}),
		imports: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "import_jobs_total",
			Help: "Jobs d'import terminés, par statut.",
		}, []string{"status"}),
	}
	m.reg.MustRegister(m.requests, m.duration, m.evaluation, m.imports,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	// Les deux statuts existent dès le départ : la série apparaît à 0.
	m.imports.WithLabelValues(ImportSucceeded)
	m.imports.WithLabelValues(ImportFailed)
	return m
}

// Handler sert le format texte Prometheus (à monter sur le port interne seulement).
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// EvaluationDone enregistre la durée d'une évaluation (hook du module stockeval).
func (m *Metrics) EvaluationDone(d time.Duration) { m.evaluation.Observe(d.Seconds()) }

// ImportFinished compte un job d'import terminé (hook du module stockeval).
func (m *Metrics) ImportFinished(status string) { m.imports.WithLabelValues(status).Inc() }

// Middleware mesure chaque requête. La route est ramenée à un nombre borné de
// valeurs (voir routeLabel) pour éviter l'explosion de cardinalité.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		route := routeLabel(r.URL.Path)
		m.requests.WithLabelValues(r.Method, route, statusClass(rec.status)).Inc()
		m.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// statusClass : 2xx, 4xx… (borne la cardinalité, suffit aux alertes).
func statusClass(code int) string {
	return strconv.Itoa(code/100) + "xx"
}

// routeLabel garde /healthz, /readyz et la ressource de /api/v1, jamais les
// identifiants ni les symboles.
func routeLabel(path string) string {
	switch path {
	case "/healthz", "/readyz":
		return path
	}
	if rest, ok := strings.CutPrefix(path, "/api/v1/"); ok {
		res, _, _ := strings.Cut(rest, "/")
		switch res {
		case "stocks", "imports", "rules":
			return "/api/v1/" + res
		}
	}
	return "autre"
}
