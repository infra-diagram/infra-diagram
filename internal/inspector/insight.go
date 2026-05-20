package inspector

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Insight es una función pura sobre el grafo que produce un Result.
//
// Cada insight tiene un identificador estable, una versión semántica del
// contrato de salida, y parámetros tipados.
type Insight interface {
	// ID estable. Forma: "{categoría}.{nombre}".
	ID() string

	// Version semántica del CONTRATO DE SALIDA del Result.
	// Bump major si cambia el shape del Result.
	Version() string

	// Metadata describe el insight para consumidores (UI, docs, CLI).
	Metadata() Metadata

	// Execute corre el insight contra el grafo.
	// `filters` aplica los filtros estándar que todo insight respeta.
	// `params` son parámetros específicos del insight (puede ser nil).
	Execute(ctx context.Context, session neo4j.SessionWithContext, filters StandardFilters, params map[string]any) (*Result, error)
}

// Metadata describe un insight para consumidores externos.
type Metadata struct {
	Title       string
	Description string
	Category    string // "structural" | "iam" | "network" | "data"
	Severity    Severity
	Parameters  []ParameterSpec // parámetros específicos (no incluye filtros estándar)
}

type ParameterSpec struct {
	Name        string
	Type        string // "string" | "arn" | "duration" | "int"
	Required    bool
	Description string
}

// StandardFilters son los filtros que TODO insight respeta. Se aplican
// dentro del Cypher para eficiencia.
//
// Campos vacíos significan "no filtrar por esto".
type StandardFilters struct {
	AccountID    string   // filtra por accountId exacto
	Region       string   // filtra por region exacto
	ResourceType string   // filtra por label específico (ej. "Queue")
	ExcludeTags  []string // excluye recursos con cualquiera de estas tags
}

// IsEmpty retorna true si no hay ningún filtro aplicado.
func (f StandardFilters) IsEmpty() bool {
	return f.AccountID == "" &&
		f.Region == "" &&
		f.ResourceType == "" &&
		len(f.ExcludeTags) == 0
}
