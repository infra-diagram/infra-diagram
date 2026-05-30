import { LAYOUT_LABELS, LAYOUT_NAMES, type LayoutName } from "./layouts";

interface Props {
  layout: LayoutName;
  onLayout: (l: LayoutName) => void;
  onFit: () => void;
  onZoom: (factor: number) => void;
  search: string;
  onSearch: (q: string) => void;
  matchCount: number | null;
}

export function GraphControls({
  layout,
  onLayout,
  onFit,
  onZoom,
  search,
  onSearch,
  matchCount,
}: Props) {
  return (
    <div className="absolute left-3 top-3 z-10 flex flex-wrap items-center gap-1.5">
      <select
        value={layout}
        onChange={(e) => onLayout(e.target.value as LayoutName)}
        className="rounded border border-border bg-bg/90 px-2 py-1 font-mono text-xs outline-none focus:border-accent"
      >
        {LAYOUT_NAMES.map((l) => (
          <option key={l} value={l}>
            {LAYOUT_LABELS[l]}
          </option>
        ))}
      </select>

      <Btn onClick={onFit}>fit</Btn>
      <Btn onClick={() => onZoom(1.2)}>+</Btn>
      <Btn onClick={() => onZoom(1 / 1.2)}>−</Btn>

      <div className="relative">
        <input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder="buscar nodo…"
          spellCheck={false}
          className="w-40 rounded border border-border bg-bg/90 px-2 py-1 font-mono text-xs outline-none focus:border-accent"
        />
        {search && matchCount !== null && (
          <span className="absolute right-2 top-1/2 -translate-y-1/2 font-mono text-[10px] text-muted">
            {matchCount}
          </span>
        )}
      </div>
    </div>
  );
}

function Btn({
  children,
  onClick,
}: {
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="rounded border border-border bg-bg/90 px-2 py-1 font-mono text-xs hover:border-accent"
    >
      {children}
    </button>
  );
}
