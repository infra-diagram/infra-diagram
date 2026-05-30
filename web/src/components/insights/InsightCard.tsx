import { Link } from "react-router-dom";
import type { Insight } from "@/api/types";
import { Badge, SeverityBadge } from "@/components/ui/primitives";

export function InsightCard({ insight }: { insight: Insight }) {
  const requiredParams = (insight.parameters ?? []).filter((p) => p.required);

  return (
    <Link
      to={`/insights/${insight.id}`}
      className="group flex flex-col gap-3 rounded-lg border border-border bg-surface p-4 transition hover:border-accent/60 hover:bg-surface-2"
    >
      <div className="flex items-start justify-between gap-2">
        <h3 className="font-semibold leading-tight group-hover:text-accent">
          {insight.title}
        </h3>
        <SeverityBadge severity={insight.severity} />
      </div>
      <p className="line-clamp-3 text-sm text-muted">{insight.description}</p>
      <div className="mt-auto flex flex-wrap items-center gap-2">
        <span className="font-mono text-[11px] text-muted">{insight.id}</span>
        {requiredParams.map((p) => (
          <Badge key={p.name}>requiere {p.name}</Badge>
        ))}
      </div>
    </Link>
  );
}
