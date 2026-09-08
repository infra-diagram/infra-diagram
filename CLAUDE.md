# infra-diagram — Contexto del proyecto

## Estructura del monorepo

infra-diagram/
├── cmd/api/        # Backend Go (chi router, puerto 8080)
├── internal/
├── docs/
├── infra/
├── web/            # Frontend (React + Vite) — NO es repo separado
└── ...

## Backend

Go, chi router, puerto 8080. Findings es la unidad primaria de la API (hash
SHA256 de 16 hex chars como ID estable, ver `internal/api/service/findings.go`):
- GET /health
- GET /insights
- GET /insights/{type}/summary?account_id={uuid}&<params>
- GET /insights/{type}/findings?account_id={uuid}&<params>[&severity=&page=&page_size=]
- GET /insights/{type}/findings/{finding_id}?account_id={uuid}&<params>
- GET /insights/{type}/findings/{finding_id}/graph?account_id={uuid}&<params>   ← subgrafo scoped a ESE finding, no el grafo completo del insight
- GET /insights/{type}/results?account_id={uuid}&<params>   ← DEPRECATED, alias de compat (sin description/remediation/evidence por finding). No usar en frontend nuevo.

`role_arn` solo aplica a `iam.blast_radius` (requerido en todos los endpoints scoped a ese insight).

**No existe un endpoint de "grafo completo del insight".** Fue reemplazado por
`/findings/{id}/graph` en la reestructuración a Findings-como-unidad-primaria.
El frontend arma la experiencia de grafo entrando siempre desde un finding
concreto (ver "Deep-link nodo ↔ finding" más abajo).

**Quirk importante:** Go serializa slices nil como `null`, no `[]`. Los campos
  `parameters`, `nodes`, `edges`, `findings` pueden llegar como `null` desde el backend.
  Ya está resuelto en `web/src/api/insights.ts` normalizando en la frontera.

**Cuidado con el proxy de Vite:** `/insights` es prefijo tanto de rutas de API
(`/insights/{type}/findings`) como de la ruta de página de React Router
(`/insights/{type}`). El proxy dev (`web/vite.config.ts`) matchea por regex
las rutas de API específicas — no agregar una entrada de prefijo plano
`"/insights"` de nuevo, rompe la navegación directa/refresh a la página del insight
(la pega contra el backend Go, que la 404ea).

## 5 insights disponibles

| id | required params |
|----|----------------|
| `structural.orphans` | ninguno |
| `iam.overpermissive_policies` | ninguno |
| `network.crossboundary` | ninguno |
| `data.access_patterns` | ninguno (`overprivileged_threshold`, `overshared_threshold` opcionales) |
| `iam.blast_radius` | `role_arn` (requerido en todos los endpoints scoped al insight) |

## Frontend — web/

### Stack
- React 19 + TypeScript estricto
- Vite 6 (proxy dev: `/health` y las rutas de API bajo `/insights` → `:8080`, por regex — ver nota de Backend)
- TailwindCSS v4 (`@theme` en `src/index.css`, sin `tailwind.config.js`)
- TanStack Query v5 (data layer)
- React Router v7
- Cytoscape.js con extensiones `cytoscape-fcose` y `cytoscape-dagre`
- TanStack Virtual (tabla de findings)

### Estructura de src/
src/
├── api/
│   ├── types.ts          # Contratos tipados 1:1 con structs Go
│   ├── client.ts         # fetch wrapper, ApiError, buildQuery
│   └── insights.ts       # funciones tipadas (summary/findings/finding/finding-graph) + normalización de nulls
├── hooks/
│   ├── useInsights.ts          # GET /insights (staleTime: Infinity)
│   ├── useFindingsSummary.ts   # GET /insights/:type/summary (badges, independiente del tab activo)
│   ├── useFindings.ts          # GET /insights/:type/findings (lazy, tab Findings)
│   ├── useFinding.ts           # GET /insights/:type/findings/:id (lazy, al expandir una fila)
│   ├── useFindingGraph.ts      # GET /insights/:type/findings/:id/graph (lazy, tab Graph + finding seleccionado)
│   └── useHealth.ts            # GET /health (refetchInterval: 30s)
├── state/
│   └── account.tsx       # Context de account_id (input simple, localStorage)
├── lib/
│   └── severity.ts       # Orden y colores de severidad (LOW/MEDIUM/HIGH/CRITICAL)
├── components/
│   ├── ErrorBoundary.tsx     # Class component, evita pantallas en blanco
│   ├── AccountBar.tsx        # Barra superior: input account_id + HealthDot
│   ├── ui/
│   │   └── primitives.tsx    # Spinner, ErrorState, EmptyState, Badge, SeverityBadge
│   ├── insights/
│   │   ├── InsightCard.tsx       # Tarjeta del catálogo
│   │   └── InsightParamsForm.tsx # Form genérico desde ParameterSpec[]
│   ├── graph/
│   │   ├── GraphCanvas.tsx       # Cytoscape ref-based, lazy-loaded
│   │   ├── GraphControls.tsx     # Selector layout, fit/zoom, búsqueda
│   │   ├── cytoscapeStyles.ts    # Stylesheet: colorForType() + clases faded/match
│   │   └── layouts.ts            # fcose, dagre, concentric + registerLayouts()
│   └── findings/
│       └── FindingsTable.tsx     # Virtualizada, filtro severidad, filas expandibles
├── pages/
│   ├── CatalogPage.tsx   # "/" — insights agrupados por categoría
│   └── InsightPage.tsx   # "/insights/:type" — tabs Graph | Findings
├── types/
│   └── cytoscape-ext.d.ts  # Declaraciones para cytoscape-fcose y cytoscape-dagre
├── App.tsx
├── main.tsx              # QueryClient + AccountProvider + BrowserRouter + ErrorBoundary
├── index.css             # @theme Tailwind v4: tema oscuro consola técnica
└── vite-env.d.ts


### Decisiones de diseño clave

**Tema visual:** consola técnica oscura. Fuentes IBM Plex Sans (UI) + IBM Plex Mono
(ARNs, IDs, tipos). Acento ámbar. Severidad con 5 colores oklch distintos (INFO/LOW/MEDIUM/HIGH/CRITICAL).

**Gating de queries:** summary/findings/graph no se disparan hasta tener `account_id` +
todos los `required: true` de `parameters` completos. El gating es derivado (useMemo),
no estado, para no desincronizarse cuando el catálogo resuelve el insight.

**Carga perezosa por tab:** el tab Graph no dispara `useFindingGraph` si estás en
Findings y viceversa (`enabled: tab === "graph" && canRun`). `useFindingsSummary`
es la excepción: corre independientemente del tab activo para mostrar la barra
de severidad apenas `canRun` es true.

**Cytoscape ref-based:** instancia en `useEffect` sobre `<div ref>`, no
`react-cytoscapejs`. Tres efectos separados: crear instancia (datos), re-layout
(layout), búsqueda (sobre instancia viva). Lazy-loaded con `React.lazy` para no
incluir los ~500 kB en el bundle del catálogo.

**Deep-link nodo ↔ finding:** el grafo está scoped a UN finding (contrato
`/findings/{id}/graph`), no es más el grafo completo del insight. Estado
compartido en `InsightPage`: `selectedFindingId` (qué subgrafo pedir) +
`selectedArn` (qué nodo/recurso está en foco, puente: `node.data.id === finding.resource.arn`).
- Findings → Grafo: botón "ver en grafo →" en finding expandido llama
  `locateInGraph(findingId, arn)` → fija `selectedFindingId` y cambia de tab.
- Grafo → Findings: click en nodo busca en `findingsByArn` (construido desde
  la lista ya cacheada de `useFindings`, no desde el subgrafo) y muestra
  TODOS los findings de ese recurso —no solo el que originó el subgrafo— en
  un panel; click en uno navega al tab Findings, expande la fila y hace scroll
  hasta ella.

**Form de parámetros genérico:** `InsightParamsForm` se construye desde
`ParameterSpec[]`. Tipos `int`/`float` → `inputMode="numeric"`, resto texto.
Insights nuevos funcionan sin tocar el componente.

**Findings virtualizados:** `useVirtualizer` de TanStack Virtual con medición
dinámica (`measureElement`) para filas expandibles de altura variable. La fila
de la lista (`FindingView`) es liviana — sin `description`/`remediation`/`evidence`;
al expandir, `FindingDetailPanel` dispara `useFinding` (lazy, `enabled: true`
solo mientras está montado) para traer el detalle completo por hash id.

### Contratos de la API

```typescript
// ParameterSpec
{ name: string; type: string; required: boolean; description: string }

// Insight
{ id: string; version: string; title: string; description: string;
  category: string; severity: "INFO"|"LOW"|"MEDIUM"|"HIGH"|"CRITICAL";
  parameters: ParameterSpec[] }  // ← puede llegar null del backend, normalizar

// ResourceRef
{ arn: string; type: string; name: string; region?: string; tags?: Record<string,string> }

// FindingsSummary — GET /insights/{type}/summary
{ insight_id: string; insight_version: string; account_id: string;
  executed_at: string; execution_ms: number; total: number;
  severity_counts: { CRITICAL: number; HIGH: number; MEDIUM: number; LOW: number; INFO: number } }

// FindingsPage — GET /insights/{type}/findings
{ insight_id: string; insight_version: string; account_id: string;
  executed_at: string; execution_ms: number;
  pagination: { page: number; page_size: number; total: number; total_pages: number };
  severity_counts: {...};
  findings: { id: string /* hash, URL-safe */; key: string /* ID canónico verboso */;
    resource: ResourceRef; severity: string; title: string; related_count: number }[] }

// FindingDetail — GET /insights/{type}/findings/{finding_id}
{ id: string; key: string; insight_id: string; insight_version: string;
  resource: ResourceRef; severity: string; title: string; description: string;
  evidence?: Record<string,unknown>; remediation?: string; related_resources?: ResourceRef[] }

// FindingGraphResponse — GET /insights/{type}/findings/{finding_id}/graph
// Subgrafo SCOPED a ese finding (anchor + related_resources), no el grafo completo del insight.
{ insight_id: string; insight_version: string; finding_id: string;
  executed_at: string; execution_ms: number;
  nodes: { data: { id: string; label: string; type: string; properties?: Record<string,unknown> } }[];
  edges: { data: { id: string; source: string; target: string; label?: string } }[] }
```

### Variables de entorno

```bash
VITE_API_BASE_URL=   # vacío en dev (proxy Vite) y prod embebido (mismo origen)
VITE_ACCOUNT_ID=     # opcional, editable en runtime desde la UI
```

### Comandos

```bash
cd web
npm install
npm run dev      # http://localhost:5173 con proxy → :8080
npm run build    # tsc -b && vite build → web/dist/
npm run typecheck
```

### Issues conocidos resueltos

- Go serializa `[]T nil` como `null` → normalizado en `api/insights.ts`
- Cytoscape no acepta ARNs con `:` como selectores CSS →
  usar `cy.getElementById(arn)` en vez de `cy.$(#${arn})`
- El frontend quedó atrasado tras la reestructuración a Findings-como-unidad-primaria
  (llamaba a `/graph` y `/results` con los shapes viejos) → migrado a
  `/summary`+`/findings`+`/findings/{id}`+`/findings/{id}/graph`
- Proxy de Vite con prefijo plano `"/insights"` colisionaba con la ruta de página
  `/insights/:type` (navegación directa/refresh devolvía el 404 del backend Go) →
  proxy por regex, solo rutas de API reales (ver nota en Backend)

## Fuera del MVP (no implementar)

- Auth / onboarding UI
- WebSockets
- Múltiples cuentas en UI (hay una sola a la vez)
- Export de grafo a imagen (pendiente para después)

## Próximos pasos pendientes

1. ~~Deep-link nodo ↔ finding~~ ✅
2. `go:embed` en `cmd/api/` para servir el frontend desde el binario Go
3. Export del grafo a imagen PNG/SVG
