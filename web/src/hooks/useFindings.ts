import { useQuery } from "@tanstack/react-query";
import { getFindings } from "@/api/insights";
import type { ParamValues } from "@/api/types";

interface Opts {
  type: string;
  accountId: string;
  params: ParamValues;
  enabled: boolean;
}

// page_size al máximo que acepta el backend (200): el filtro de severidad
// sigue siendo client-side en FindingsTable, así que traemos todo de una vez
// en lugar de armar paginación server-side en la UI.
const MAX_PAGE_SIZE = 200;

export function useFindings({ type, accountId, params, enabled }: Opts) {
  return useQuery({
    queryKey: ["findings", type, accountId, params],
    queryFn: () => getFindings(type, accountId, params, { page: 1, pageSize: MAX_PAGE_SIZE }),
    enabled,
    staleTime: 5 * 60_000, // ejecuciones caras: no re-disparar al volver al tab
    gcTime: 30 * 60_000,
    retry: 1,
  });
}
