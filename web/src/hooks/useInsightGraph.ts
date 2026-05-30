import { useQuery } from "@tanstack/react-query";
import { getInsightGraph } from "@/api/insights";
import type { ParamValues } from "@/api/types";

interface Opts {
  type: string;
  accountId: string;
  params: ParamValues;
  // El caller decide si disparar: típicamente tab activo && cuenta && required completos.
  enabled: boolean;
}

// La query key incluye type + accountId + params -> cambiar un threshold o el
// role_arn produce una entrada de cache distinta (correcto).
export function useInsightGraph({ type, accountId, params, enabled }: Opts) {
  return useQuery({
    queryKey: ["graph", type, accountId, params],
    queryFn: () => getInsightGraph(type, accountId, params),
    enabled,
    staleTime: 5 * 60_000, // ejecuciones caras: no re-disparar al volver al tab
    gcTime: 30 * 60_000,
    retry: 1,
  });
}
