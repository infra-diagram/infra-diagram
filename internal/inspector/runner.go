package inspector

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Runner ejecuta insights y rellena la metadata estándar del Result.
type Runner struct {
	driver neo4j.DriverWithContext
}

func NewRunner(driver neo4j.DriverWithContext) *Runner {
	return &Runner{driver: driver}
}

// Run ejecuta un insight con los filtros y parámetros dados, y rellena la
// metadata del Result (ID, version, timestamps, execution time)
//
// La sesión se abre en modo Read porque los insights son consultas, no
// escriben al grafo
func (r *Runner) Run(ctx context.Context, insight Insight, filters StandardFilters, params map[string]any) (*Result, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	start := time.Now()
	result, err := insight.Execute(ctx, session, filters, params)
	if err != nil {
		return nil, fmt.Errorf("executing insight %s: %w", insight.ID(), err)
	}
	if result == nil {
		// Defensa: insight devolvió (nil, nil). Tratamos como result vacío.
		result = &Result{Findings: []Finding{}, Subgraph: Subgraph{}}
	}

	result.InsightID = insight.ID()
	result.InsightVersion = insight.Version()
	result.ExecutedAt = time.Now().UTC()
	result.ExecutionMS = time.Since(start).Milliseconds()

	return result, nil
}
