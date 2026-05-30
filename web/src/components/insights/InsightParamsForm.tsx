import { useState } from "react";
import type { ParameterSpec, ParamValues } from "@/api/types";

// Form genérico: se construye desde el array `parameters` del catálogo.
// Cualquier insight nuevo "just works" sin tocar este componente.
// Solo emite los valores parseados; el gating (required completos) se deriva
// en la página, que conoce el insight resuelto.

interface Props {
  parameters: ParameterSpec[];
  values: ParamValues;
  onChange: (values: ParamValues) => void;
}

function isNumeric(type: string) {
  return type === "int" || type === "float" || type === "number";
}

export function InsightParamsForm({ parameters, values, onChange }: Props) {
  const [local, setLocal] = useState<Record<string, string>>(() => {
    const init: Record<string, string> = {};
    for (const p of parameters) {
      const v = values[p.name];
      init[p.name] = v === undefined ? "" : String(v);
    }
    return init;
  });

  function emit(next: Record<string, string>) {
    setLocal(next);
    const parsed: ParamValues = {};
    for (const p of parameters) {
      const raw = next[p.name]?.trim();
      if (!raw) continue;
      parsed[p.name] = isNumeric(p.type) ? Number(raw) : raw;
    }
    onChange(parsed);
  }

  if (parameters.length === 0) return null;

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <p className="mb-3 font-mono text-xs uppercase tracking-wide text-muted">
        Parámetros
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        {parameters.map((p) => (
          <label key={p.name} className="flex flex-col gap-1">
            <span className="flex items-center gap-1.5 font-mono text-xs">
              {p.name}
              {p.required ? (
                <span className="text-sev-high">*</span>
              ) : (
                <span className="text-muted">(opcional)</span>
              )}
            </span>
            <input
              value={local[p.name] ?? ""}
              inputMode={isNumeric(p.type) ? "numeric" : "text"}
              onChange={(e) => emit({ ...local, [p.name]: e.target.value })}
              placeholder={p.description}
              spellCheck={false}
              className="rounded border border-border bg-bg px-2.5 py-1.5 font-mono text-xs outline-none focus:border-accent"
            />
            <span className="text-[11px] text-muted">{p.description}</span>
          </label>
        ))}
      </div>
    </div>
  );
}
