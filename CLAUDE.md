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

Go, chi router, puerto 8080
4 endpoints disponibles:
- GET /health
- GET /insights
- GET /insights/{type}/results?account_id={uuid}&<params>
- GET /insights/{type}/graph?account_id={uuid}&<params>   ← role_arn solo para blast_radius

**Quirk importante:** Go serializa slices nil como `null`, no `[]`. Los campos
  `parameters`, `nodes`, `edges`, `findings` pueden llegar como `null` desde el backend.
  Ya está resuelto en `web/src/api/insights.ts` normalizando en la frontera.

## 5 insights disponibles

| id | required params |
|----|----------------|
| `structural.orphans` | ninguno |
| `iam.overpermissive_policies` | ninguno |
| `network.crossboundary` | ninguno |
| `data.access_patterns` | ninguno (`overprivileged_threshold`, `overshared_threshold` opcionales) |
| `iam.blast_radius` | `role_arn` (requerido en `/graph` Y en `/results`) |

## Frontend — web/

### Stack
- React 19 + TypeScript estricto
- Vite 6 (proxy dev: `/health` y `/insights` → `:8080`)
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
│   └── insights.ts       # 4 funciones tipadas + normalización de nulls
├── hooks/
│   ├── useInsights.ts        # GET /insights (staleTime: Infinity)
│   ├── useInsightGraph.ts    # GET /insights/:type/graph (lazy, enabled prop)
│   ├── useInsightResults.ts  # GET /insights/:type/results (lazy, enabled prop)
│   └── useHealth.ts          # GET /health (refetchInterval: 30s)
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
(ARNs, IDs, tipos). Acento ámbar. Severidad con 4 colores oklch distintos.

**Gating de queries:** graph y results no se disparan hasta tener `account_id` +
todos los `required: true` de `parameters` completos. El gating es derivado (useMemo),
no estado, para no desincronizarse cuando el catálogo resuelve el insight.

**Carga perezosa por tab:** el tab Graph no dispara la query si estás en Findings y
viceversa. `enabled: tab === "graph" && canRun`.

**Cytoscape ref-based:** instancia en `useEffect` sobre `<div ref>`, no
`react-cytoscapejs`. Tres efectos separados: crear instancia (datos), re-layout
(layout), búsqueda (sobre instancia viva). Lazy-loaded con `React.lazy` para no
incluir los ~500 kB en el bundle del catálogo.

**Deep-link nodo ↔ finding:** `selectedArn` es estado compartido en `InsightPage`.
Puente: `node.data.id === finding.resource.arn`. 
- Grafo → Findings: click en nodo muestra findings relacionados en panel, click en
  uno navega al tab Findings, expande la fila y hace scroll hasta ella.
- Findings → Grafo: botón "ver en grafo →" en finding expandido, centra y selecciona
  el nodo en el canvas.

**Form de parámetros genérico:** `InsightParamsForm` se construye desde
`ParameterSpec[]`. Tipos `int`/`float` → `inputMode="numeric"`, resto texto.
Insights nuevos funcionan sin tocar el componente.

**Findings virtualizados:** `useVirtualizer` de TanStack Virtual con medición
dinámica (`measureElement`) para filas expandibles de altura variable.

### Contratos de la API

```typescript
// ParameterSpec
{ name: string; type: string; required: boolean; description: string }

// Insight
{ id: string; version: string; title: string; description: string;
  category: string; severity: "LOW"|"MEDIUM"|"HIGH"|"CRITICAL";
  parameters: ParameterSpec[] }  // ← puede llegar null del backend, normalizar

// GraphResponse
{ insight_id: string; insight_version: string; executed_at: string;
  execution_ms: number;
  nodes: { data: { id: string; label: string; type: string; properties?: Record<string,unknown> } }[];
  edges: { data: { id: string; source: string; target: string; label?: string } }[] }

// ResultsResponse
{ insight_id: string; findings_count: number;
  findings: { id: string; resource: { arn: string; type: string; name: string };
    severity: string; title: string; description: string;
    remediation: string; evidence: Record<string,unknown> }[] }
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

## Fuera del MVP (no implementar)

- Auth / onboarding UI
- WebSockets
- Múltiples cuentas en UI (hay una sola a la vez)
- Export de grafo a imagen (pendiente para después)

## Próximos pasos pendientes

1. ~~Deep-link nodo ↔ finding~~ ✅
2. `go:embed` en `cmd/api/` para servir el frontend desde el binario Go
3. Export del grafo a imagen PNG/SVG
