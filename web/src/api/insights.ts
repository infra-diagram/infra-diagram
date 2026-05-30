import { apiGet } from "./client";
import type {
  GraphResponse,
  HealthResponse,
  Insight,
  ParamValues,
  ResultsResponse,
} from "./types";

// NORMALIZACIÓN EN LA FRONTERA.
// Go serializa los slices nil como `null`, no `[]`. Para que el resto de la app
// pueda confiar en los tipos (y no reventar con `.filter`/spread sobre null),
// coercionamos los arrays a [] acá, una sola vez.

// GET /health  (sin account_id)
export function getHealth() {
  return apiGet<HealthResponse>("/health");
}

// GET /insights  (catálogo; sin account_id)
export async function getInsights(): Promise<Insight[]> {
  const data = await apiGet<Insight[]>("/insights");
  return (data ?? []).map((i) => ({ ...i, parameters: i.parameters ?? [] }));
}

// GET /insights/{type}/graph?account_id=...&<params>
// params lleva role_arn / thresholds / lo que declare el ParameterSpec.
export async function getInsightGraph(
  type: string,
  accountId: string,
  params: ParamValues = {},
): Promise<GraphResponse> {
  const data = await apiGet<GraphResponse>(`/insights/${type}/graph`, {
    account_id: accountId,
    ...params,
  });
  return { ...data, nodes: data.nodes ?? [], edges: data.edges ?? [] };
}

// GET /insights/{type}/results?account_id=...&<params>
// OJO: los params required (ej. role_arn) también aplican aquí, no solo en /graph.
export async function getInsightResults(
  type: string,
  accountId: string,
  params: ParamValues = {},
): Promise<ResultsResponse> {
  const data = await apiGet<ResultsResponse>(`/insights/${type}/results`, {
    account_id: accountId,
    ...params,
  });
  return { ...data, findings: data.findings ?? [] };
}
