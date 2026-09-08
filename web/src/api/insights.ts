import { apiGet } from "./client";
import type {
  FindingDetail,
  FindingGraphResponse,
  FindingsPage,
  FindingsSummary,
  HealthResponse,
  Insight,
  ParamValues,
  Severity,
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

// GET /insights/{type}/summary?account_id=...&<params>
export function getFindingsSummary(
  type: string,
  accountId: string,
  params: ParamValues = {},
): Promise<FindingsSummary> {
  return apiGet<FindingsSummary>(`/insights/${type}/summary`, {
    account_id: accountId,
    ...params,
  });
}

export interface ListFindingsOptions {
  severity?: Severity;
  page?: number;
  pageSize?: number;
}

// GET /insights/{type}/findings?account_id=...&severity=&page=&page_size=&<params>
export async function getFindings(
  type: string,
  accountId: string,
  params: ParamValues = {},
  opts: ListFindingsOptions = {},
): Promise<FindingsPage> {
  const data = await apiGet<FindingsPage>(`/insights/${type}/findings`, {
    account_id: accountId,
    severity: opts.severity,
    page: opts.page,
    page_size: opts.pageSize,
    ...params,
  });
  return { ...data, findings: data.findings ?? [] };
}

// GET /insights/{type}/findings/{finding_id}?account_id=...&<params>
export function getFinding(
  type: string,
  accountId: string,
  findingId: string,
  params: ParamValues = {},
): Promise<FindingDetail> {
  return apiGet<FindingDetail>(`/insights/${type}/findings/${findingId}`, {
    account_id: accountId,
    ...params,
  });
}

// GET /insights/{type}/findings/{finding_id}/graph?account_id=...&<params>
export async function getFindingGraph(
  type: string,
  accountId: string,
  findingId: string,
  params: ParamValues = {},
): Promise<FindingGraphResponse> {
  const data = await apiGet<FindingGraphResponse>(
    `/insights/${type}/findings/${findingId}/graph`,
    { account_id: accountId, ...params },
  );
  return { ...data, nodes: data.nodes ?? [], edges: data.edges ?? [] };
}
