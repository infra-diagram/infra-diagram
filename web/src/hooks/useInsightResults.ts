import { useQuery } from "@tanstack/react-query";
import { getInsightResults } from "@/api/insights";
import type { ParamValues } from "@/api/types";

interface Opts {
  type: string;
  accountId: string;
  params: ParamValues;
  enabled: boolean;
}

export function useInsightResults({ type, accountId, params, enabled }: Opts) {
  return useQuery({
    queryKey: ["results", type, accountId, params],
    queryFn: () => getInsightResults(type, accountId, params),
    enabled,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
    retry: 1,
  });
}
