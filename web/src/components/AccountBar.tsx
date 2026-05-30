import { useState } from "react";
import { Link } from "react-router-dom";
import { useAccount } from "@/state/account";
import { useHealth } from "@/hooks/useHealth";

function HealthDot() {
  const { isSuccess, isError, isLoading } = useHealth();
  const color = isLoading
    ? "var(--color-muted)"
    : isSuccess
      ? "var(--color-sev-low)"
      : "var(--color-sev-high)";
  const label = isLoading ? "comprobando" : isSuccess ? "online" : "sin conexión";
  return (
    <span className="flex items-center gap-1.5 font-mono text-xs text-muted">
      <span className="size-2 rounded-full" style={{ backgroundColor: color }} />
      backend {label}
      {isError && null}
    </span>
  );
}

export function AccountBar() {
  const { accountId, setAccountId } = useAccount();
  const [draft, setDraft] = useState(accountId);

  return (
    <header className="sticky top-0 z-10 border-b border-border bg-bg/80 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center gap-4 px-6 py-3">
        <Link to="/" className="font-mono text-sm font-semibold tracking-tight">
          infra<span className="text-accent">·</span>diagram
        </Link>

        <div className="ml-auto flex items-center gap-3">
          <HealthDot />
          <div className="flex items-center gap-2">
            <label htmlFor="account" className="font-mono text-xs text-muted">
              account_id
            </label>
            <input
              id="account"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onBlur={() => setAccountId(draft)}
              onKeyDown={(e) => e.key === "Enter" && setAccountId(draft)}
              placeholder="uuid de la cuenta"
              spellCheck={false}
              className="w-72 rounded border border-border bg-surface px-2.5 py-1 font-mono text-xs outline-none focus:border-accent"
            />
          </div>
        </div>
      </div>
    </header>
  );
}
