import { useQuery } from "@tanstack/react-query";
import { getFinding } from "@/api/insights";
import type { ParamValues } from "@/api/types";

interface Opts {
  type: string;
  accountId: string;
  findingId: string;
  params: ParamValues;
  // Fetch perezoso: solo al expandir la fila en FindingsTable.
  enabled: boolean;
}

export function useFinding({ type, accountId, findingId, params, enabled }: Opts) {
  return useQuery({
    queryKey: ["finding", type, accountId, findingId, params],
    queryFn: () => getFinding(type, accountId, findingId, params),
    enabled,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
    retry: 1,
  });
}
