import {
  createContext,
  useCallback,
  useContext,
  useState,
  type ReactNode,
} from "react";

// account_id es un input simple enviado en cada request de graph/results.
// Multi-cuenta está fuera del MVP: una sola cuenta a la vez.
// Semilla: VITE_ACCOUNT_ID (opcional) -> localStorage -> editable en runtime.

const STORAGE_KEY = "infra-diagram.account_id";

interface AccountCtx {
  accountId: string;
  setAccountId: (id: string) => void;
}

const Ctx = createContext<AccountCtx | null>(null);

function initialAccount(): string {
  const fromStorage =
    typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
  return fromStorage ?? import.meta.env.VITE_ACCOUNT_ID ?? "";
}

export function AccountProvider({ children }: { children: ReactNode }) {
  const [accountId, setAccountIdState] = useState<string>(initialAccount);

  const setAccountId = useCallback((id: string) => {
    const trimmed = id.trim();
    setAccountIdState(trimmed);
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(STORAGE_KEY, trimmed);
    }
  }, []);

  return (
    <Ctx.Provider value={{ accountId, setAccountId }}>{children}</Ctx.Provider>
  );
}

export function useAccount(): AccountCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useAccount debe usarse dentro de <AccountProvider>");
  return ctx;
}
