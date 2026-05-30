import { useQuery } from "@tanstack/react-query";
import { getInsights } from "@/api/insights";

// El catálogo no cambia durante la sesión: cache agresivo, no necesita account_id.
export function useInsights() {
  return useQuery({
    queryKey: ["insights"],
    queryFn: getInsights,
    staleTime: Infinity,
  });
}
