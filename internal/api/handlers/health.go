package handlers

import (
	"net/http"

	"infra-diagram/internal/api/render"
)

// Health responds with the current health status of the API.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}
