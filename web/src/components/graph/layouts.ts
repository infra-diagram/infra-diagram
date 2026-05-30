import cytoscape from "cytoscape";
import fcose from "cytoscape-fcose";
import dagre from "cytoscape-dagre";

// Registrar extensiones una sola vez (idempotente).
let registered = false;
export function registerLayouts() {
  if (registered) return;
  cytoscape.use(fcose);
  cytoscape.use(dagre);
  registered = true;
}

export const LAYOUT_NAMES = ["fcose", "dagre", "concentric"] as const;
export type LayoutName = (typeof LAYOUT_NAMES)[number];

export const LAYOUT_LABELS: Record<LayoutName, string> = {
  fcose: "Orgánico (fcose)",
  dagre: "Jerárquico (dagre)",
  concentric: "Concéntrico",
};

// fcose: bueno para topología de infra (relaciones no jerárquicas).
// dagre: bueno cuando la dirección importa (TRIGGERS, flujos).
export function layoutConfig(name: LayoutName): cytoscape.LayoutOptions {
  const base = { animate: false as const, fit: true, padding: 30 };
  switch (name) {
    case "fcose":
      return {
        name: "fcose",
        ...base,
        quality: "default",
        randomize: true,
        nodeRepulsion: 6000,
        idealEdgeLength: 80,
        nodeSeparation: 90,
      } as unknown as cytoscape.LayoutOptions;
    case "dagre":
      return {
        name: "dagre",
        ...base,
        rankDir: "LR",
        nodeSep: 30,
        rankSep: 70,
      } as unknown as cytoscape.LayoutOptions;
    case "concentric":
      return {
        name: "concentric",
        ...base,
        minNodeSpacing: 30,
        concentric: (n: cytoscape.NodeSingular) => n.degree(false),
        levelWidth: () => 2,
      } as unknown as cytoscape.LayoutOptions;
  }
}
