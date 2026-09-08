import { useQuery } from "@tanstack/react-query";
import { getFindingsSummary } from "@/api/insights";
import type { ParamValues } from "@/api/types";

interface Opts {
  type: string;
  accountId: string;
  params: ParamValues;
  enabled: boolean;
}

export function useFindingsSummary({ type, accountId, params, enabled }: Opts) {
  return useQuery({
    queryKey: ["findings-summary", type, accountId, params],
    queryFn: () => getFindingsSummary(type, accountId, params),
    enabled,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
    retry: 1,
  });
}
