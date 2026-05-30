// Package api wires the HTTP server, routes, and dependencies.
package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"infra-diagram/internal/api/handlers"
	"infra-diagram/internal/api/middleware"
	"infra-diagram/internal/api/service"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	addr    string
	httpSrv *http.Server
}

// New constructs a Server with the given service and listening address.
func New(addr string, svc *service.Service) *Server {
	h := handlers.New(svc)

	r := chi.NewRouter()

	// Standard middleware
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.Logging)
	r.Use(middleware.Recovery)

	// Routes
	r.Get("/health", h.Health)

	r.Route("/insights", func(r chi.Router) {
		r.Get("/", h.ListInsights)

		r.Route("/{type}", func(r chi.Router) {
			r.Get("/summary", h.FindingsSummary)

			r.Route("/findings", func(r chi.Router) {
				r.Get("/", h.ListFindings)
				r.Get("/{finding_id}", h.GetFinding)
				r.Get("/{finding_id}/graph", h.GetFindingGraph)
			})

			// DEPRECATED — remove after frontend migrates off /results.
			r.Get("/results", h.InsightResults)
		})
	})
	return &Server{
		addr: addr,
		httpSrv: &http.Server{
			Addr:    addr,
			Handler: r,
		},
	}
}

// Start blocks listening on the configured address.
func (s *Server) Start() error {
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}
	return nil
}
