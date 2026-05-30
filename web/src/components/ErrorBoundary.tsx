import { Component, type ErrorInfo, type ReactNode } from "react";

// Sin esto, cualquier excepción de render deja la página en blanco (solo visible
// en la consola). Con esto, el error se muestra en pantalla.
interface State {
  error: Error | null;
}

export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Render error:", error, info);
  }

  render() {
    if (this.state.error) {
      return (
        <div className="mx-auto max-w-2xl px-6 py-12">
          <div className="rounded-lg border border-sev-high/40 bg-sev-high/5 p-5">
            <p className="font-semibold text-sev-high">La interfaz encontró un error</p>
            <p className="mt-1 text-sm text-muted">
              Algo falló al renderizar. Detalle:
            </p>
            <pre className="mt-3 overflow-auto rounded bg-surface p-3 font-mono text-[11px] text-muted">
              {this.state.error.message}
            </pre>
            <button
              onClick={() => this.setState({ error: null })}
              className="mt-3 rounded border border-border px-3 py-1 text-sm hover:bg-surface-2"
            >
              Reintentar
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
