package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"infra-diagram/internal/api/render"
	"infra-diagram/internal/api/service"
	"infra-diagram/internal/inspector"
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
	result, ok := h.runInsight(w, r)
	if !ok {
		return
	}
	render.JSON(w, http.StatusOK, map[string]any{
		"insight_id":      result.InsightID,
		"insight_version": result.InsightVersion,
		"executed_at":     result.ExecutedAt,
		"execution_ms":    result.ExecutionMS,
		"findings":        result.Findings,
	})
}

// InsightGraph runs an insight and returns its subgraph in Cytoscape format.
// GET /insights/{type}/graph?account_id={id}
func (h *Handlers) InsightGraph(w http.ResponseWriter, r *http.Request) {
	result, ok := h.runInsight(w, r)
	if !ok {
		return
	}
	graph := render.ToCytoscape(result.Subgraph)
	render.JSON(w, http.StatusOK, map[string]any{
		"insight_id":      result.InsightID,
		"insight_version": result.InsightVersion,
		"executed_at":     result.ExecutedAt,
		"execution_ms":    result.ExecutionMS,
		"nodes":           graph.Nodes,
		"edges":           graph.Edges,
	})
}

// runInsight is the shared logic for both /results and /graph:
// parse params, execute the insight, handle errors. Returns the Result
// and true on success; writes the error response and returns false on failure.
func (h *Handlers) runInsight(w http.ResponseWriter, r *http.Request) (*inspector.Result, bool) {
	accountID, err := parseAccountID(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return nil, false
	}

	insightType := insightTypeFromURL(r)
	extraParams := parseExtraParams(r)

	result, err := h.service.RunInsight(r.Context(), accountID, insightType, extraParams)
	if err != nil {
		mapServiceError(w, err)
		return nil, false
	}

	return result, true
}

// parseExtraParams collects insight-specific query parameters.
//
// Known insight parameters are extracted from query string. We don't have
// a generic schema-driven parser yet, so for the MVP we collect the params
// that current insights use: role_arn, overprivileged_threshold,
// overshared_threshold.
//
// Unknown params are ignored. Each insight validates its own required params.
func parseExtraParams(r *http.Request) map[string]any {
	params := map[string]any{}
	q := r.URL.Query()

	if v := q.Get("role_arn"); v != "" {
		params["role_arn"] = v
	}
	if v := q.Get("overprivileged_threshold"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			params["overprivileged_threshold"] = n
		}
	}
	if v := q.Get("overshared_threshold"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			params["overshared_threshold"] = n
		}
	}

	return params
}

// === Shared helpers ===

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

func insightTypeFromURL(r *http.Request) string {
	return chi.URLParam(r, "type")
}

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
