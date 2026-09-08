import type { Severity } from "@/api/types";

export const SEVERITY_ORDER: Record<Severity, number> = {
  INFO: 0,
  LOW: 1,
  MEDIUM: 2,
  HIGH: 3,
  CRITICAL: 4,
};

// Mapea a la variable CSS definida en index.css (@theme).
export const SEVERITY_COLOR_VAR: Record<Severity, string> = {
  INFO: "var(--color-sev-info)",
  LOW: "var(--color-sev-low)",
  MEDIUM: "var(--color-sev-medium)",
  HIGH: "var(--color-sev-high)",
  CRITICAL: "var(--color-sev-critical)",
};

export function compareSeverityDesc(a: Severity, b: Severity): number {
  return SEVERITY_ORDER[b] - SEVERITY_ORDER[a];
}
