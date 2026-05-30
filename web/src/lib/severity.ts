import type { Severity } from "@/api/types";

export const SEVERITY_ORDER: Record<Severity, number> = {
  LOW: 0,
  MEDIUM: 1,
  HIGH: 2,
  CRITICAL: 3,
};

// Mapea a la variable CSS definida en index.css (@theme).
export const SEVERITY_COLOR_VAR: Record<Severity, string> = {
  LOW: "var(--color-sev-low)",
  MEDIUM: "var(--color-sev-medium)",
  HIGH: "var(--color-sev-high)",
  CRITICAL: "var(--color-sev-critical)",
};

export function compareSeverityDesc(a: Severity, b: Severity): number {
  return SEVERITY_ORDER[b] - SEVERITY_ORDER[a];
}
