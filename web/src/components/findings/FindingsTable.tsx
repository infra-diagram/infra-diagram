import { useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { Finding, Severity } from "@/api/types";
import { compareSeverityDesc } from "@/lib/severity";
import { SeverityBadge } from "@/components/ui/primitives";

const SEVERITIES: Severity[] = ["CRITICAL", "HIGH", "MEDIUM", "LOW"];

interface Props {
  findings: Finding[];
  total: number;
  // Deep-link
  selectedArn: string | null;
  pendingFindingId: string | null;
  onLocateInGraph: (arn: string) => void;
}

// Virtualizada: a 250+ findings solo se montan las filas visibles.
// Medición dinámica para soportar filas expandibles (altura variable).
export function FindingsTable({
  findings,
  total,
  selectedArn,
  pendingFindingId,
  onLocateInGraph,
}: Props) {
  const [filter, setFilter] = useState<Severity | "ALL">("ALL");
  const [expanded, setExpanded] = useState<string | null>(null);
  const parentRef = useRef<HTMLDivElement>(null);

  const rows = useMemo(() => {
    const filtered =
      filter === "ALL" ? findings : findings.filter((f) => f.severity === filter);
    return [...filtered].sort((a, b) => compareSeverityDesc(a.severity, b.severity));
  }, [findings, filter]);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 45, // alto de fila colapsada; las expandidas se miden solas
    overscan: 8,
    getItemKey: (i) => rows[i].id,
  });

  // grafo -> findings: cuando llega un finding desde el grafo, mostrarlo (quita
  // filtro), expandirlo y desplazar hasta él.
  useEffect(() => {
    if (!pendingFindingId) return;
    setFilter("ALL");
    setExpanded(pendingFindingId);
  }, [pendingFindingId]);

  useEffect(() => {
    if (!pendingFindingId) return;
    const idx = rows.findIndex((r) => r.id === pendingFindingId);
    if (idx >= 0) virtualizer.scrollToIndex(idx, { align: "center" });
  }, [pendingFindingId, rows, virtualizer]);

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="font-mono text-xs text-muted">
          {rows.length} de {total} findings
        </span>
        <div className="ml-auto flex gap-1">
          {(["ALL", ...SEVERITIES] as const).map((s) => (
            <button
              key={s}
              onClick={() => setFilter(s)}
              className={`rounded border px-2 py-0.5 font-mono text-[11px] transition ${
                filter === s
                  ? "border-accent text-accent"
                  : "border-border text-muted hover:border-fg/40"
              }`}
            >
              {s}
            </button>
          ))}
        </div>
      </div>

      <div
        ref={parentRef}
        className="h-[70vh] overflow-auto rounded-lg border border-border"
      >
        <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((vi) => {
            const f = rows[vi.index];
            const open = expanded === f.id;
            const focused = selectedArn != null && f.resource.arn === selectedArn;
            return (
              <div
                key={vi.key}
                data-index={vi.index}
                ref={virtualizer.measureElement}
                className={`absolute left-0 top-0 w-full border-b border-border ${
                  focused ? "bg-accent/5 ring-1 ring-inset ring-accent/50" : ""
                }`}
                style={{ transform: `translateY(${vi.start}px)` }}
              >
                <button
                  onClick={() => setExpanded(open ? null : f.id)}
                  className="flex w-full items-center gap-3 px-4 py-2.5 text-left hover:bg-surface"
                >
                  <SeverityBadge severity={f.severity} />
                  <span className="flex-1 truncate text-sm">{f.title}</span>
                  <span className="hidden truncate font-mono text-[11px] text-muted sm:block sm:max-w-[40%]">
                    {f.resource.name}
                  </span>
                  <span className="text-muted">{open ? "−" : "+"}</span>
                </button>
                {open && (
                  <div className="space-y-3 bg-bg px-4 py-3 text-sm">
                    <Field label="resource">
                      <div className="flex items-start justify-between gap-3">
                        <span className="break-all font-mono text-xs">
                          {f.resource.type} · {f.resource.arn}
                        </span>
                        <button
                          onClick={() => onLocateInGraph(f.resource.arn)}
                          className="shrink-0 rounded border border-border px-2 py-0.5 font-mono text-[11px] hover:border-accent hover:text-accent"
                        >
                          ver en grafo →
                        </button>
                      </div>
                    </Field>
                    <Field label="description">{f.description}</Field>
                    <Field label="remediation">{f.remediation}</Field>
                    <Field label="evidence">
                      <pre className="overflow-auto rounded bg-surface p-2 font-mono text-[11px] text-muted">
                        {JSON.stringify(f.evidence, null, 2)}
                      </pre>
                    </Field>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-1 sm:grid-cols-[120px_1fr]">
      <span className="font-mono text-[11px] uppercase tracking-wide text-muted">
        {label}
      </span>
      <div className="text-fg">{children}</div>
    </div>
  );
}
