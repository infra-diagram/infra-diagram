import { useEffect, useRef, useState } from "react";
import cytoscape from "cytoscape";
import type { Finding, GraphResponse } from "@/api/types";
import { buildStylesheet } from "./cytoscapeStyles";
import { layoutConfig, registerLayouts, type LayoutName } from "./layouts";
import { GraphControls } from "./GraphControls";
import { SeverityBadge } from "@/components/ui/primitives";

registerLayouts();

interface Props {
  data: GraphResponse;
  // Deep-link: ARN en foco (compartido con la tabla de findings).
  selectedArn: string | null;
  onSelectNode: (arn: string | null) => void;
  relatedFindings: Finding[];
  findingsLoaded: boolean;
  onOpenFinding: (findingId: string, arn: string) => void;
}

// Wrapper ref-based: control total del ciclo de vida (init, layout, eventos, fit).
export function GraphCanvas({
  data,
  selectedArn,
  onSelectNode,
  relatedFindings,
  findingsLoaded,
  onOpenFinding,
}: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const cyRef = useRef<cytoscape.Core | null>(null);
  const layoutRef = useRef<LayoutName>("fcose");
  const [layout, setLayout] = useState<LayoutName>("fcose");
  const [search, setSearch] = useState("");
  const [matchCount, setMatchCount] = useState<number | null>(null);

  // Nodo en foco derivado del ARN compartido (no estado interno).
  const selectedNode =
    selectedArn != null
      ? (data.nodes.find((n) => n.data.id === selectedArn)?.data ?? null)
      : null;

  // Crear instancia cuando cambian los datos.
  useEffect(() => {
    if (!containerRef.current) return;

    const cy = cytoscape({
      container: containerRef.current,
      elements: [...data.nodes, ...data.edges],
      style: buildStylesheet(),
      layout: layoutConfig(layoutRef.current),
      wheelSensitivity: 0.2,
    });
    cyRef.current = cy;

    cy.on("tap", "node", (evt) => onSelectNode(evt.target.id()));
    cy.on("tap", (evt) => {
      if (evt.target === cy) onSelectNode(null);
    });

    return () => cy.destroy();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  // Re-correr layout al cambiar la selección de layout (sin recrear la instancia).
  useEffect(() => {
    layoutRef.current = layout;
    cyRef.current?.layout(layoutConfig(layout)).run();
  }, [layout]);

  // Aplicar el foco compartido: seleccionar y centrar el nodo (también tras
  // recrear la instancia, p. ej. al volver del tab Findings con un ARN ya elegido).
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy) return;
    cy.elements().unselect();
    if (!selectedArn) return;
    const n = cy.getElementById(selectedArn); // por id crudo: evita parsear el ARN como selector
    if (n.nonempty()) {
      n.select();
      cy.animate({ center: { eles: n }, zoom: Math.max(cy.zoom(), 1.2) }, { duration: 250 });
    }
  }, [selectedArn, data]);

  // Búsqueda: atenúa lo no coincidente y resalta lo coincidente.
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy) return;
    const q = search.trim().toLowerCase();

    if (!q) {
      cy.batch(() => cy.elements().removeClass("faded match"));
      setMatchCount(null);
      return;
    }

    const matches = cy.nodes().filter((n) => {
      const label = String(n.data("label") ?? "").toLowerCase();
      const id = String(n.data("id") ?? "").toLowerCase();
      return label.includes(q) || id.includes(q);
    });

    cy.batch(() => {
      cy.elements().addClass("faded").removeClass("match");
      matches.removeClass("faded").addClass("match");
      matches.connectedEdges().removeClass("faded");
    });
    setMatchCount(matches.length);
  }, [search, data]);

  return (
    <div className="relative h-[70vh] overflow-hidden rounded-lg border border-border bg-surface">
      <div ref={containerRef} className="size-full" />

      <GraphControls
        layout={layout}
        onLayout={setLayout}
        onFit={() => cyRef.current?.fit(undefined, 30)}
        onZoom={(f) => cyRef.current?.zoom(cyRef.current.zoom() * f)}
        search={search}
        onSearch={setSearch}
        matchCount={matchCount}
      />

      <div className="absolute right-3 top-3 z-10 font-mono text-[11px] text-muted">
        {data.nodes.length} nodos · {data.edges.length} edges · {data.execution_ms}ms
      </div>

      {selectedNode && (
        <aside className="absolute bottom-3 right-3 z-10 flex max-h-[70%] w-80 flex-col overflow-hidden rounded-lg border border-border bg-bg/95 backdrop-blur">
          <div className="flex items-center justify-between border-b border-border px-3 py-2">
            <span className="font-mono text-xs text-accent">{selectedNode.type}</span>
            <button
              onClick={() => onSelectNode(null)}
              className="text-muted hover:text-fg"
            >
              ×
            </button>
          </div>

          <div className="overflow-auto p-3">
            <p className="mb-3 break-all font-mono text-xs">{selectedNode.id}</p>

            {selectedNode.properties && (
              <dl className="mb-3 space-y-1">
                {Object.entries(selectedNode.properties).map(([k, v]) => (
                  <div key={k} className="grid grid-cols-[1fr_1.4fr] gap-2 text-[11px]">
                    <dt className="truncate font-mono text-muted">{k}</dt>
                    <dd className="break-all font-mono">{String(v)}</dd>
                  </div>
                ))}
              </dl>
            )}

            {/* Deep-link: findings de este recurso */}
            <div className="border-t border-border pt-2">
              <p className="mb-1.5 font-mono text-[11px] uppercase tracking-wide text-muted">
                Findings ({relatedFindings.length})
              </p>
              {!findingsLoaded ? (
                <p className="text-[11px] text-muted">
                  Ejecutá el tab Findings para ver hallazgos relacionados.
                </p>
              ) : relatedFindings.length === 0 ? (
                <p className="text-[11px] text-muted">Sin findings para este recurso.</p>
              ) : (
                <ul className="space-y-1">
                  {relatedFindings.map((f) => (
                    <li key={f.id}>
                      <button
                        onClick={() => onOpenFinding(f.id, f.resource.arn)}
                        className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left hover:bg-surface-2"
                      >
                        <SeverityBadge severity={f.severity} />
                        <span className="flex-1 truncate text-xs">{f.title}</span>
                        <span className="text-muted">→</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </aside>
      )}
    </div>
  );
}
