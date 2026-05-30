package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"infra-diagram/internal/api/render"
	"infra-diagram/internal/api/service"
	"infra-diagram/internal/inspector"
)

const (
	defaultPage     = 1
	defaultPageSize = 25
	maxPageSize     = 200
)

// ListInsights returns the catalog of available insights.
// GET /insights
func (h *Handlers) ListInsights(w http.ResponseWriter, r *http.Request) {
	insights := h.service.ListInsights(r.Context())
	render.JSON(w, http.StatusOK, insights)
}

// FindingsSummary returns severity counts + total for an insight (dashboard).
// GET /insights/{type}/summary?account_id={id}
func (h *Handlers) FindingsSummary(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountID(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	sum, err := h.service.GetFindingsSummary(
		r.Context(), accountID, insightTypeFromURL(r), parseExtraParams(r),
	)
	if err != nil {
		mapServiceError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, sum)
}

// ListFindings returns a paginated, severity-filtered list of finding views.
// GET /insights/{type}/findings?account_id={id}[&severity=HIGH&page=1&page_size=25]
func (h *Handlers) ListFindings(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountID(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	severity, err := parseSeverity(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	page, pageSize := parsePagination(r)

	res, err := h.service.ListFindings(
		r.Context(), accountID, insightTypeFromURL(r), parseExtraParams(r),
		service.ListFindingsOptions{Severity: severity, Page: page, PageSize: pageSize},
	)
	if err != nil {
		mapServiceError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, res)
}

// GetFinding returns the full detail for a single finding.
// GET /insights/{type}/findings/{finding_id}?account_id={id}
func (h *Handlers) GetFinding(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountID(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	detail, err := h.service.GetFinding(
		r.Context(), accountID, insightTypeFromURL(r),
		chi.URLParam(r, "finding_id"), parseExtraParams(r),
	)
	if err != nil {
		mapServiceError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, detail)
}

// GetFindingGraph returns the subgraph of a single finding in Cytoscape format.
// GET /insights/{type}/findings/{finding_id}/graph?account_id={id}
func (h *Handlers) GetFindingGraph(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountID(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	g, err := h.service.GetFindingGraph(
		r.Context(), accountID, insightTypeFromURL(r),
		chi.URLParam(r, "finding_id"), parseExtraParams(r),
	)
	if err != nil {
		mapServiceError(w, err)
		return
	}

	graph := render.ToCytoscape(g.Subgraph)
	render.JSON(w, http.StatusOK, map[string]any{
		"insight_id":      g.InsightID,
		"insight_version": g.InsightVersion,
		"finding_id":      g.FindingID,
		"executed_at":     g.ExecutedAt,
		"execution_ms":    g.ExecutionMS,
		"nodes":           graph.Nodes,
		"edges":           graph.Edges,
	})
}

// InsightResults is DEPRECATED. Kept as a thin alias during migration so
// existing clients don't break. New clients should use /findings.
// Remove once the frontend has migrated. Returns the unpaginated full list.
// GET /insights/{type}/results?account_id={id}
func (h *Handlers) InsightResults(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountID(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	res, err := h.service.ListFindings(
		r.Context(), accountID, insightTypeFromURL(r), parseExtraParams(r),
		service.ListFindingsOptions{Page: 1, PageSize: maxPageSize},
	)
	if err != nil {
		mapServiceError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, res)
}

// --- param parsing ---

func parsePagination(r *http.Request) (page, pageSize int) {
	page, pageSize = defaultPage, defaultPageSize
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxPageSize {
				n = maxPageSize
			}
			pageSize = n
		}
	}
	return page, pageSize
}

func parseSeverity(r *http.Request) (inspector.Severity, error) {
	v := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("severity")))
	if v == "" {
		return "", nil
	}
	switch inspector.Severity(v) {
	case inspector.SeverityInfo, inspector.SeverityLow, inspector.SeverityMedium,
		inspector.SeverityHigh, inspector.SeverityCritical:
		return inspector.Severity(v), nil
	default:
		return "", errors.New("invalid severity; must be one of INFO, LOW, MEDIUM, HIGH, CRITICAL")
	}
}

// parseExtraParams collects insight-specific query parameters.
// Unknown params are ignored; each insight validates its own required params.
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
	case errors.Is(err, service.ErrFindingNotFound):
		render.Error(w, http.StatusNotFound, "finding not found")
	case errors.Is(err, service.ErrMissingParam):
		render.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrInvalidParam):
		render.Error(w, http.StatusBadRequest, err.Error())
	default:
		render.Error(w, http.StatusInternalServerError, "internal server error")
	}
}
