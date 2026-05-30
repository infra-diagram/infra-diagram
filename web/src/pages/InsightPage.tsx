import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useInsights } from "@/hooks/useInsights";
import { useInsightGraph } from "@/hooks/useInsightGraph";
import { useInsightResults } from "@/hooks/useInsightResults";
import { useAccount } from "@/state/account";
import { InsightParamsForm } from "@/components/insights/InsightParamsForm";
import { FindingsTable } from "@/components/findings/FindingsTable";
import { EmptyState, ErrorState, SeverityBadge, Spinner } from "@/components/ui/primitives";
import type { Finding, ParamValues } from "@/api/types";

// Cytoscape (~500 kB) solo se carga cuando se renderiza el grafo, no en el catálogo.
const GraphCanvas = lazy(() =>
  import("@/components/graph/GraphCanvas").then((m) => ({ default: m.GraphCanvas })),
);

type Tab = "graph" | "findings";

export function InsightPage() {
  const { type = "" } = useParams();
  const { accountId } = useAccount();
  const { data: catalog } = useInsights();
  const insight = catalog?.find((i) => i.id === type);

  const [tab, setTab] = useState<Tab>("graph");
  const [params, setParams] = useState<ParamValues>({});

  // --- Estado compartido del deep-link: el recurso (ARN) en foco ---
  // node.data.id (grafo) === finding.resource.arn (tabla). Ese ARN es el puente.
  const [selectedArn, setSelectedArn] = useState<string | null>(null);
  // Finding a abrir/desplazar cuando se entra al tab Findings desde el grafo.
  const [pendingFindingId, setPendingFindingId] = useState<string | null>(null);

  // Resetear el foco al cambiar de insight.
  useEffect(() => {
    setSelectedArn(null);
    setPendingFindingId(null);
  }, [type]);

  const requiredComplete = useMemo(() => {
    const req = insight?.parameters.filter((p) => p.required) ?? [];
    return req.every((p) => {
      const v = params[p.name];
      return v !== undefined && String(v).trim() !== "";
    });
  }, [insight, params]);

  const hasAccount = accountId.trim().length > 0;
  const canRun = hasAccount && requiredComplete;

  const graph = useInsightGraph({
    type,
    accountId,
    params,
    enabled: tab === "graph" && canRun,
  });
  const results = useInsightResults({
    type,
    accountId,
    params,
    enabled: tab === "findings" && canRun,
  });

  // Findings agrupados por ARN para lookup O(1) desde el panel del grafo.
  const findingsByArn = useMemo(() => {
    const m = new Map<string, Finding[]>();
    for (const f of results.data?.findings ?? []) {
      const arr = m.get(f.resource.arn) ?? [];
      arr.push(f);
      m.set(f.resource.arn, arr);
    }
    return m;
  }, [results.data]);

  const relatedFindings = selectedArn ? (findingsByArn.get(selectedArn) ?? []) : [];

  // grafo -> findings: abrir un finding desde el panel del nodo.
  function openFinding(findingId: string, arn: string) {
    setSelectedArn(arn);
    setPendingFindingId(findingId);
    setTab("findings");
  }

  // findings -> grafo: ubicar el recurso de un finding en el grafo.
  function locateInGraph(arn: string) {
    setSelectedArn(arn);
    setTab("graph");
  }

  const blockedReason = useMemo(() => {
    if (!hasAccount) return "Definí un account_id en la barra superior para ejecutar.";
    if (!requiredComplete) return "Completá los parámetros requeridos (*) para ejecutar.";
    return null;
  }, [hasAccount, requiredComplete]);

  if (!insight) {
    return (
      <div className="mx-auto max-w-6xl px-6 py-8">
        <Spinner label="Resolviendo insight…" />
        <p className="mt-4 text-sm text-muted">
          Si no aparece, volvé al{" "}
          <Link to="/" className="text-accent underline">
            catálogo
          </Link>
          .
        </p>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <Link to="/" className="font-mono text-xs text-muted hover:text-accent">
        ← catálogo
      </Link>

      <div className="mt-3 mb-5 flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold">{insight.title}</h1>
            <SeverityBadge severity={insight.severity} />
          </div>
          <p className="mt-1 max-w-2xl text-sm text-muted">{insight.description}</p>
          <p className="mt-1 font-mono text-[11px] text-muted">
            {insight.id} · v{insight.version}
          </p>
        </div>
      </div>

      <div className="mb-5">
        <InsightParamsForm
          parameters={insight.parameters}
          values={params}
          onChange={(v) => setParams(v)}
        />
      </div>

      {/* Tabs */}
      <div className="mb-4 flex gap-1 border-b border-border">
        {(["graph", "findings"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`-mb-px border-b-2 px-4 py-2 font-mono text-sm transition ${
              tab === t
                ? "border-accent text-fg"
                : "border-transparent text-muted hover:text-fg"
            }`}
          >
            {t === "graph" ? "Graph" : "Findings"}
          </button>
        ))}
      </div>

      {blockedReason ? (
        <EmptyState title="Listo para ejecutar" hint={blockedReason} />
      ) : tab === "graph" ? (
        <TabState
          query={graph}
          render={(d) =>
            d.nodes.length === 0 ? (
              <EmptyState title="Sin nodos" />
            ) : (
              <Suspense fallback={<Spinner label="Cargando visor de grafo…" />}>
                <GraphCanvas
                  data={d}
                  selectedArn={selectedArn}
                  onSelectNode={setSelectedArn}
                  relatedFindings={relatedFindings}
                  findingsLoaded={!!results.data}
                  onOpenFinding={openFinding}
                />
              </Suspense>
            )
          }
        />
      ) : (
        <TabState
          query={results}
          render={(d) =>
            d.findings_count === 0 ? (
              <EmptyState title="0 findings" hint="Nada que reportar. 🎉" />
            ) : (
              <FindingsTable
                findings={d.findings}
                total={d.findings_count}
                selectedArn={selectedArn}
                pendingFindingId={pendingFindingId}
                onLocateInGraph={locateInGraph}
              />
            )
          }
        />
      )}
    </div>
  );
}

// Pequeño helper para no repetir el árbol loading/error/render en cada tab.
function TabState<T>({
  query,
  render,
}: {
  query: {
    isLoading: boolean;
    isError: boolean;
    error: unknown;
    data: T | undefined;
    refetch: () => void;
  };
  render: (data: T) => React.ReactNode;
}) {
  if (query.isLoading) return <Spinner label="Ejecutando insight…" />;
  if (query.isError)
    return (
      <ErrorState
        message={(query.error as Error)?.message}
        onRetry={() => query.refetch()}
      />
    );
  if (!query.data) return null;
  return <>{render(query.data)}</>;
}
