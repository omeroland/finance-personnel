package observability

import (
	"net/http"
	"time"
)

// DefaultMetricsAddr est le port interne des métriques : jamais publié par
// Compose ni routé par Traefik.
const DefaultMetricsAddr = ":9090"

// NewMetricsServer sert /metrics seulement, sur son propre port.
func NewMetricsServer(addr string, m *Metrics) *http.Server {
	if addr == "" {
		addr = DefaultMetricsAddr
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
}
