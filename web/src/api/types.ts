// Contratos del backend Go. Modelados 1:1 con las structs para que un cambio
// de contrato falle en compile-time y no en runtime.

export type Severity = "INFO" | "LOW" | "MEDIUM" | "HIGH" | "CRITICAL";

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

export interface ResourceRef {
  arn: string;
  type: string;
  name: string;
  region?: string;
  tags?: Record<string, string>;
}

export interface SeverityCounts {
  CRITICAL: number;
  HIGH: number;
  MEDIUM: number;
  LOW: number;
  INFO: number;
}

export interface Pagination {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

// Fila liviana de /insights/{type}/findings — sin evidence/remediation/description.
export interface FindingView {
  id: string; // hash estable, URL-safe — usar para /findings/{id} y /findings/{id}/graph
  key: string; // ID canónico del insight (verboso, no URL-safe)
  resource: ResourceRef;
  severity: Severity;
  title: string;
  related_count: number;
}

// GET /insights/{type}/findings/{finding_id}
export interface FindingDetail {
  id: string;
  key: string;
  insight_id: string;
  insight_version: string;
  resource: ResourceRef;
  severity: Severity;
  title: string;
  description: string;
  evidence?: Record<string, unknown>;
  remediation?: string;
  related_resources?: ResourceRef[];
}

// GET /insights/{type}/findings?account_id=...&severity=&page=&page_size=
export interface FindingsPage {
  insight_id: string;
  insight_version: string;
  account_id: string;
  executed_at: string;
  execution_ms: number;
  pagination: Pagination;
  severity_counts: SeverityCounts;
  findings: FindingView[];
}

// GET /insights/{type}/summary
export interface FindingsSummary {
  insight_id: string;
  insight_version: string;
  account_id: string;
  executed_at: string;
  execution_ms: number;
  total: number;
  severity_counts: SeverityCounts;
}

// GET /insights/{type}/findings/{finding_id}/graph
export interface FindingGraphResponse {
  insight_id: string;
  insight_version: string;
  finding_id: string;
  executed_at: string;
  execution_ms: number;
  nodes: CyNode[];
  edges: CyEdge[];
}

// GET /health
export interface HealthResponse {
  status?: string;
  [k: string]: unknown;
}
