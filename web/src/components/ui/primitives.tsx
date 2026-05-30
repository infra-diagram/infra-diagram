import type { ReactNode } from "react";
import type { Severity } from "@/api/types";
import { SEVERITY_COLOR_VAR } from "@/lib/severity";

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-3 text-muted">
      <span className="size-4 animate-spin rounded-full border-2 border-border border-t-accent" />
      {label && <span className="text-sm">{label}</span>}
    </div>
  );
}

export function ErrorState({
  title = "Algo falló",
  message,
  onRetry,
}: {
  title?: string;
  message?: string;
  onRetry?: () => void;
}) {
  return (
    <div className="rounded-lg border border-sev-high/40 bg-sev-high/5 p-4">
      <p className="font-semibold text-sev-high">{title}</p>
      {message && <p className="mt-1 text-sm text-muted">{message}</p>}
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-3 rounded border border-border px-3 py-1 text-sm hover:bg-surface-2"
        >
          Reintentar
        </button>
      )}
    </div>
  );
}

export function EmptyState({
  title,
  hint,
}: {
  title: string;
  hint?: ReactNode;
}) {
  return (
    <div className="rounded-lg border border-dashed border-border p-8 text-center">
      <p className="font-medium">{title}</p>
      {hint && <p className="mt-1 text-sm text-muted">{hint}</p>}
    </div>
  );
}

export function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded border border-border bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] uppercase tracking-wide text-muted">
      {children}
    </span>
  );
}

export function SeverityBadge({ severity }: { severity: Severity }) {
  const color = SEVERITY_COLOR_VAR[severity];
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wide"
      style={{ color, backgroundColor: `color-mix(in oklch, ${color} 14%, transparent)` }}
    >
      <span className="size-1.5 rounded-full" style={{ backgroundColor: color }} />
      {severity}
    </span>
  );
}
