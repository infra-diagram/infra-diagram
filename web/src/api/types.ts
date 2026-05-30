// Contratos del backend Go. Modelados 1:1 con las structs para que un cambio
// de contrato falle en compile-time y no en runtime.

export type Severity = "LOW" | "MEDIUM" | "HIGH" | "CRITICAL";

export type ParameterType = "int" | "float" | "string" | "bool" | "arn";

// Espejo de ParameterSpec (Go). Maneja los dos tipos de insight:
//  - required: false  -> opcional (ej. thresholds de data.access_patterns)
//  - required: true   -> obligatorio antes de poder ejecutar (ej. role_arn en blast_radius)
export interface ParameterSpec {
  name: string;
  type: ParameterType | string; // tolerante a tipos nuevos del backend
  required: boolean;
  description: string;
}

// GET /insights
export interface Insight {
  id: string;
  version: string;
  title: string;
  description: string;
  category: string;
  severity: Severity;
  parameters: ParameterSpec[];
}

// Valores resueltos de un form de parámetros: { role_arn: "arn:...", overprivileged_threshold: 10 }
export type ParamValues = Record<string, string | number | boolean>;

// --- Cytoscape: el backend ya devuelve el shape { data: {...} } que cy espera ---
export interface CyNodeData {
  id: string;
  label: string;
  type: string;
  properties?: Record<string, unknown>;
}

export interface CyEdgeData {
  id: string;
  source: string;
  target: string;
  label?: string;
}

export interface CyNode {
  data: CyNodeData;
}
export interface CyEdge {
  data: CyEdgeData;
}

// GET /insights/{type}/graph
export interface GraphResponse {
  insight_id: string;
  insight_version: string;
  executed_at: string;
  execution_ms: number;
  nodes: CyNode[];
  edges: CyEdge[];
}

// GET /insights/{type}/results
export interface Finding {
  id: string;
  resource: { arn: string; type: string; name: string };
  severity: Severity;
  title: string;
  description: string;
  remediation: string;
  evidence: Record<string, unknown>;
}

export interface ResultsResponse {
  insight_id: string;
  findings_count: number;
  findings: Finding[];
}

// GET /health
export interface HealthResponse {
  status?: string;
  [k: string]: unknown;
}
