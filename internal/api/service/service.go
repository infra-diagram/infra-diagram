package service

import (
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/store"
)

// Service holds the dependencies needed to handle API requests.
//
// It is the only place that knows about all three layers: HTTP-bound
// parameter parsing, the inspector engine, and the persistence store.
type Service struct {
	registry *inspector.Registry
	runner   *inspector.Runner
	store    store.Store
}

// New creates a Service with the given dependencies.
func New(
	registry *inspector.Registry,
	driver neo4j.DriverWithContext,
	store store.Store,
) *Service {
	return &Service{
		registry: registry,
		runner:   inspector.NewRunner(driver),
		store:    store,
	}
}
