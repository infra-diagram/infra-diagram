import type cytoscape from "cytoscape";

// Paleta determinística por tipo de recurso (hash del string -> tono).
// Evita tener que mantener un mapa manual de tipos.
function colorForType(type: string): string {
  let h = 0;
  for (let i = 0; i < type.length; i++) h = (h * 31 + type.charCodeAt(i)) % 360;
  const l = Math.max(0.65, 0.72);
  const c = Math.max(0.12, 0.13);
  return `oklch(${l} ${c} ${h})`;
}

const readVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim();

export function buildStylesheet(): cytoscape.StylesheetStyle[] {
  const border = readVar("--color-border") || "#444";
  const fg = readVar("--color-fg") || "#eee";
  const accent = readVar("--color-accent") || "#e0a44a";

  return [
    {
      selector: "node",
      style: {
        "background-color": (ele: cytoscape.NodeSingular) =>
          colorForType(ele.data("type") ?? "default"),
        label: "data(label)",
        color: fg,
        "font-family": "IBM Plex Mono, monospace",
        "font-size": 9,
        "text-valign": "bottom",
        "text-margin-y": 4,
        "text-wrap": "ellipsis",
        "text-max-width": "90px",
        "text-background-color": "var(--color-bg)",
        "text-background-opacity": 0.75,
        "text-background-padding": "3px",
        "text-background-shape": "roundrectangle",
        width: 18,
        height: 18,
        "border-width": 1,
        "border-color": border,
      },
    },
    {
      selector: "node:selected",
      style: {
        "border-width": 3,
        "border-color": accent,
        width: 24,
        height: 24,
      },
    },
    {
      selector: "edge",
      style: {
        width: 1,
        "line-color": border,
        "target-arrow-color": border,
        "target-arrow-shape": "triangle",
        "arrow-scale": 0.8,
        "curve-style": "bezier",
        label: "data(label)",
        "font-family": "IBM Plex Mono, monospace",
        "font-size": 7,
        color: "var(--color-muted)",
        "text-rotation": "autorotate",
        "text-background-color": "var(--color-bg)",
        "text-background-opacity": 0.8,
        "text-background-padding": "2px",
      },
    },
    // Estados para la búsqueda: atenuar lo no coincidente, resaltar lo coincidente.
    {
      selector: ".faded",
      style: { opacity: 0.15, "text-opacity": 0.1 },
    },
    {
      selector: "node.match",
      style: {
        "border-width": 3,
        "border-color": accent,
        "background-color": accent,
        width: 24,
        height: 24,
        "z-index": 10,
      },
    },
  ];
}
