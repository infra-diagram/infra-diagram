// Cliente HTTP mínimo. En dev, BASE = "" y las rutas relativas pegan contra el
// proxy de Vite (-> :8080). En prod embebido, mismo origen, también "".
const BASE = import.meta.env.VITE_API_BASE_URL ?? "";

// Error normalizado: la UI distingue "cargando" de "falló" sin adivinar.
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public body?: unknown,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type QueryValue = string | number | boolean | undefined | null;

function buildQuery(params?: Record<string, QueryValue>): string {
  if (!params) return "";
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "") continue;
    sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}

export async function apiGet<T>(
  path: string,
  params?: Record<string, QueryValue>,
): Promise<T> {
  const url = `${BASE}${path}${buildQuery(params)}`;

  let res: Response;
  try {
    res = await fetch(url, { headers: { Accept: "application/json" } });
  } catch (e) {
    // Fallo de red / backend caído.
    throw new ApiError(0, "No se pudo contactar al backend", e);
  }

  if (!res.ok) {
    let body: unknown;
    const text = await res.text().catch(() => "");
    try {
      body = text ? JSON.parse(text) : undefined;
    } catch {
      body = text;
    }
    const msg =
      (body && typeof body === "object" && "message" in body
        ? String((body as Record<string, unknown>).message)
        : undefined) ?? `Error ${res.status} en ${path}`;
    throw new ApiError(res.status, msg, body);
  }

  return (await res.json()) as T;
}
