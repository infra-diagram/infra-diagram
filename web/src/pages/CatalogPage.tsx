import { useMemo } from "react";
import { useInsights } from "@/hooks/useInsights";
import { InsightCard } from "@/components/insights/InsightCard";
import { ErrorState, Spinner } from "@/components/ui/primitives";
import type { Insight } from "@/api/types";

export function CatalogPage() {
  const { data, isLoading, isError, error, refetch } = useInsights();

  const grouped = useMemo(() => {
    const map = new Map<string, Insight[]>();
    for (const i of data ?? []) {
      const arr = map.get(i.category) ?? [];
      arr.push(i);
      map.set(i.category, arr);
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [data]);

  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <div className="mb-6">
        <h1 className="text-xl font-bold">Catálogo de insights</h1>
        <p className="mt-1 text-sm text-muted">
          Análisis de solo lectura sobre el grafo de infraestructura.
        </p>
      </div>

      {isLoading && <Spinner label="Cargando catálogo…" />}
      {isError && (
        <ErrorState message={(error as Error).message} onRetry={() => refetch()} />
      )}

      {grouped.map(([category, insights]) => (
        <section key={category} className="mb-8">
          <h2 className="mb-3 font-mono text-xs uppercase tracking-wide text-accent">
            {category}
          </h2>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {insights.map((i) => (
              <InsightCard key={i.id} insight={i} />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
