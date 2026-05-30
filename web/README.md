# web/ — frontend del MVP de infra-diagram

React + Vite + TailwindCSS v4 + Cytoscape.js. Vive **dentro del monorepo Go**,
junto a `cmd/api/`. Solo lectura: catálogo de insights, grafo y findings.

## Stack

- React 19 + TypeScript (contratos tipados 1:1 con las structs Go)
- Vite 6
- TailwindCSS v4 (config en `@theme` dentro de `src/index.css`, sin `tailwind.config.js`)
- TanStack Query v5 (data layer: cache, loading/error, deduplicación)
- React Router v7
- Cytoscape.js (wrapper ref-based, cargado de forma perezosa)

## Desarrollo

El backend Go corre en `:8080`. Vite proxea `/health` e `/insights` hacia ahí,
así el cliente usa rutas relativas y no hay CORS.

```bash
cd web
cp .env.example .env   # opcional: setear VITE_ACCOUNT_ID
npm install
npm run dev            # http://localhost:5173
```

`account_id` se edita en runtime desde la barra superior (persistido en
localStorage). No hay endpoint de cuentas todavía: una sola cuenta a la vez.

## Producción: embeber en el binario (go:embed)

La idea es servir el build desde el mismo binario en `:8080` → mismo origen,
cero CORS, cero proxy. Pasos sugeridos:

1. Buildear hacia una carpeta que el paquete Go pueda embeber:
   ```bash
   npm run build                 # genera web/dist
   ```
2. En `cmd/api/` (o un paquete `web`), embeber y servir el SPA:
   ```go
   //go:embed all:dist
   var dist embed.FS

   // montar como fallback del router chi, después de las rutas /insights y /health:
   sub, _ := fs.Sub(dist, "dist")
   fileServer := http.FileServer(http.FS(sub))
   r.Handle("/*", spaFallback(sub, fileServer)) // sirve index.html en rutas del cliente
   ```
   `spaFallback` debe devolver `index.html` para rutas que no sean archivos
   (porque usamos `BrowserRouter`, p. ej. `/insights/structural.orphans`).
3. Ajustar el `outDir` en `vite.config.ts` si preferís buildear directo a
   `cmd/api/dist` en lugar de `web/dist`.

## Estructura

```
src/
  api/        types.ts (contratos) · client.ts (fetch+errores) · insights.ts (endpoints)
  hooks/      useInsights · useInsightGraph · useInsightResults · useHealth
  state/      account.tsx (account_id en contexto)
  lib/        severity.ts (orden + colores)
  components/ ui · insights (card + params form genérico) · graph · findings
  pages/      CatalogPage · InsightPage (tabs Graph|Findings con gating de params)
```

## Decisiones clave

- **Form de parámetros genérico**: se construye desde el array `parameters` del
  catálogo (`ParameterSpec`). Insights nuevos funcionan sin tocar el front.
- **Gating**: graph y results no se disparan hasta tener `account_id` + todos los
  parámetros `required` (ej. `role_arn` en `blast_radius`). Los required aplican a
  **ambos** endpoints, no solo a `/graph`.
- **Carga perezosa por tab**: el grafo (caro) no se ejecuta si solo querés findings.
- **Cytoscape ref-based + lazy**: control del ciclo de vida y bundle del catálogo liviano.
- **Findings virtualizados** (TanStack Virtual): solo se montan las filas visibles,
  con medición dinámica para las filas expandibles.
- **Layouts intercambiables**: fcose (orgánico), dagre (jerárquico, LR) y concéntrico,
  más búsqueda de nodos con resaltado.

## Pendiente (próximos pases)

- Exportar el grafo a PNG/SVG.
- Resaltar findings desde el grafo y viceversa (deep-link nodo ↔ finding).
- Tests (Vitest + Testing Library) sobre el gating de parámetros y el cliente.
