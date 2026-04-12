package crawler

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Cada servicio de AWS implementa esta interfaz
type Crawler interface {
	// IngestNodes primero - sin relaciones todavía
	IngestNodes(ctx context.Context, sessions neo4j.SessionWithContext) error

	// IngestRelations después - asume que los nodos ya existen
	IngestRelations(ctx context.Context, session neo4j.SessionWithContext) error
}
