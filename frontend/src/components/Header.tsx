import type { ReactNode } from "react";
import type { Board } from "../state/useBoard";
import { Icon, Logo } from "./ui";

export function formatVersion(v: string): string {
  if (!v) return "";
  if (v === "dev") return "dev";
  if (/^v\d/.test(v)) return v;
  if (/^\d/.test(v)) return `v${v}`;
  return v;
}

interface Props {
  board: Board;
  onNewWorkspace: () => void;
  onPalette: () => void;
  onSettings: () => void;
  onTogglePlanner: () => void;
  /** Room for the update pill (milestone WI-06). */
  updateSlot?: ReactNode;
}

export function Header({ board, onNewWorkspace, onPalette, onSettings, onTogglePlanner, updateSlot }: Props) {
  const { visible, activeId, data, actions } = board;
  const needs = (wsId: string) => {
    const d = data[wsId];
    if (!d) return 0;
    const ids = new Set(d.columns.filter((c) => c.role === "needs-you").map((c) => c.id));
    return d.cards.filter((c) => ids.has(c.column_id)).length;
  };
  return (
    <header className="top">
      <div className="brand">
        <Logo />
        <span className="wordmark">Hadron</span>
        <span className="ver mono" data-testid="version">{formatVersion(board.version)}</span>
      </div>
      <button className="ibtn only-narrow" onClick={onTogglePlanner} aria-label="Open the planner chat"><Icon name="chat" /></button>
      <div className="wsbar" role="tablist" aria-label="Workspaces">
        {visible.map((w) => {
          const k = needs(w.id);
          return (
            <button key={w.id} role="tab" aria-selected={w.id === activeId} className={"ws" + (w.id === activeId ? " on" : "")} onClick={() => void actions.switchWorkspace(w.id)}>
              <span className="dot" style={{ background: w.color }} />
              {w.name}
              {k ? <span className="pill bad" aria-label={`${k} need you`}>{k}</span> : null}
            </button>
          );
        })}
        <button className="ws" onClick={onNewWorkspace} aria-label="New workspace"><Icon name="plus" size={14} /></button>
      </div>
      <div className="spacer" />
      {updateSlot}
      <button className="ibtn" onClick={onPalette} aria-label="Command palette"><Icon name="search" /></button>
      <button className="ibtn" onClick={() => void actions.setTheme(isDark(board.theme) ? "light" : "dark")} aria-label="Switch light or dark"><Icon name="moon" /></button>
      <button className="ibtn" onClick={onSettings} aria-label="Settings"><Icon name="gear" /></button>
    </header>
  );
}

export function isDark(theme: string): boolean {
  if (theme === "dark") return true;
  if (theme === "light") return false;
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}
