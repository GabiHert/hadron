import { Component } from "react";
import type { ErrorInfo, ReactNode } from "react";

interface Props {
  children: ReactNode;
}

/** Catches a render crash so the window shows what happened instead of going blank. */
export class ErrorBoundary extends Component<Props, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Hadron UI crashed:", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="empty" role="alert" style={{ margin: "auto", padding: 32, maxWidth: 520, display: "flex", flexDirection: "column", gap: 10, alignItems: "center", textAlign: "center" }}>
        <b>Something went wrong in the window.</b>
        <span className="hint">{this.state.error.message}</span>
        <span className="hint">Your sessions and data are untouched: they live in the background. Reload to carry on.</span>
        <button className="btn pri" onClick={() => window.location.reload()}>Reload</button>
      </div>
    );
  }
}
