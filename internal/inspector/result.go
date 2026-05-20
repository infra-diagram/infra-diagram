package inspector

import "time"

// Result es el payload de salida estandarizado para todos los insights.
//
// Estructura inspirada en OCSF / AWS Security Hub ASFF, pero deliberadamente
// más simple. Migrar a OCSF completo más adelante es trivial — todos los
// nombres de campo coinciden.
type Result struct {
	// Identidad — rellenada por el Runner, no por el insight
	InsightID      string    `json:"insight_id"`
	InsightVersion string    `json:"insight_version"`
	ExecutedAt     time.Time `json:"executed_at"`
	ExecutionMS    int64     `json:"execution_ms"`

	// Findings — un hallazgo por recurso
	Findings []Finding `json:"findings"`

	// Subgraph agregado — nodos y aristas para visualización
	Subgraph Subgraph `json:"subgraph"`
}

// Finding es un hallazgo individual. Por convención, un finding describe
// UN recurso problemático (no agregados).
type Finding struct {
	// ID estable: típicamente "{insightID}/{resourceARN}".
	// Permite deduplicar entre corridas y trackear lifecycle.
	ID string `json:"id"`

	Resource         ResourceRef    `json:"resource"`
	Severity         Severity       `json:"severity"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Evidence         map[string]any `json:"evidence,omitempty"`
	Remediation      string         `json:"remediation,omitempty"`
	RelatedResources []ResourceRef  `json:"related_resources,omitempty"`
}

type ResourceRef struct {
	ARN    string            `json:"arn"`
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Region string            `json:"region,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

type Subgraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ARN        string         `json:"arn"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

type GraphEdge struct {
	FromARN    string         `json:"from"`
	ToARN      string         `json:"to"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)
