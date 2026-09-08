import { useQuery } from "@tanstack/react-query";
import { getFindingGraph } from "@/api/insights";
import type { ParamValues } from "@/api/types";

interface Opts {
  type: string;
  accountId: string;
  findingId: string | null;
  params: ParamValues;
  // El caller decide si disparar: tab "graph" activo, cuenta + required completos,
  // y un finding efectivamente seleccionado.
  enabled: boolean;
}

// La query key incluye findingId -> seleccionar otro finding produce una
// entrada de cache distinta (correcto).
export function useFindingGraph({ type, accountId, findingId, params, enabled }: Opts) {
  return useQuery({
    queryKey: ["finding-graph", type, accountId, findingId, params],
    queryFn: () => getFindingGraph(type, accountId, findingId as string, params),
    enabled: enabled && findingId != null,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
    retry: 1,
  });
}
