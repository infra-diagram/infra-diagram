package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"infra-diagram/internal/api/render"
	"infra-diagram/internal/api/service"
)

// ListInsights returns the catalog of available insights.
// GET /insights
func (h *Handlers) ListInsights(w http.ResponseWriter, r *http.Request) {
	insights := h.service.ListInsights(r.Context())
	render.JSON(w, http.StatusOK, insights)
}

// InsightResults runs an insight and returns its findings.
// GET /insights/{type}/results?account_id={id}
func (h *Handlers) InsightResults(w http.ResponseWriter, r *http.Request) {
	render.Error(w, http.StatusNotImplemented, "not implemented yet")
}

// InsightGraph runs an insight and returns its subgraph in Cytoscape format.
// GET /insights/{type}/graph?account_id={id}
func (h *Handlers) InsightGraph(w http.ResponseWriter, r *http.Request) {
	render.Error(w, http.StatusNotImplemented, "not implemented yet")
}

// === Shared helpers (used when we implement the endpoints) ===

// parseAccountID extracts and validates the account_id query parameter.
func parseAccountID(r *http.Request) (uuid.UUID, error) {
	raw := r.URL.Query().Get("account_id")
	if raw == "" {
		return uuid.Nil, errors.New("missing account_id query parameter")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("invalid account_id format")
	}
	return id, nil
}

// insightTypeFromURL extracts the {type} URL param from the chi context.
func insightTypeFromURL(r *http.Request) string {
	return chi.URLParam(r, "type")
}

// mapServiceError translates service-layer errors to HTTP responses.
// Use it in handlers like:
//
//	if err != nil {
//	    mapServiceError(w, err)
//	    return
//	}
func mapServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrAccountNotFound):
		render.Error(w, http.StatusNotFound, "account not found")
	case errors.Is(err, service.ErrInsightNotFound):
		render.Error(w, http.StatusNotFound, "insight not found")
	case errors.Is(err, service.ErrMissingParam):
		render.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrInvalidParam):
		render.Error(w, http.StatusBadRequest, err.Error())
	default:
		render.Error(w, http.StatusInternalServerError, "internal server error")
	}
}
