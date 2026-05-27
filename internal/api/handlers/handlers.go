// Package handlers contains the HTTP handlers for the API.
package handlers

import "infra-diagram/internal/api/service"

// Handlers groups all HTTP handlers and their shared dependencies.
type Handlers struct {
	service *service.Service
}

// New creates a Handlers instance.
func New(svc *service.Service) *Handlers {
	return &Handlers{service: svc}
}
