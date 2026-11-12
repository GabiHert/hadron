import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import type { Board } from "../state/useBoard";
import { Icon } from "./ui";
import { withChord } from "../keys";
import { themeActions } from "../theme";
import { focusEl, onUIRequest, wsTabEl } from "../commands";
import { DeleteWorkspace, RenameField, TabMenu } from "./WorkspaceManage";
import type { TabMenuAt } from "./WorkspaceManage";

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
  onFeedback: () => void;
  onSettings: () => void;
  onTogglePlanner: () => void;
  /** Room for the update pill (milestone WI-06). */
  updateSlot?: ReactNode;
}

export function Header({ board, onNewWorkspace, onPalette, onFeedback, onSettings, onTogglePlanner, updateSlot }: Props) {
  const { visible, activeId, data, actions } = board;
  const [editing, setEditing] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [menu, setMenu] = useState<TabMenuAt | null>(null);
  const [drag, setDrag] = useState<{ id: string; over: string | null } | null>(null);
  const dragId = useRef<string | null>(null);
  useEffect(
    () =>
      onUIRequest((r) => {
        if (r.do === "workspace.rename") setEditing(r.id);
        if (r.do === "workspace.delete") setDeleting(r.id);
      }),
    [],
  );
  const refocus = (id: string) => setTimeout(() => focusEl(wsTabEl(id)), 0);
  const closeMenu = useCallback((back: boolean) => {
    setMenu((m) => {
      if (m && back) setTimeout(() => focusEl(wsTabEl(m.id)), 0);
      return null;
    });
  }, []);
  const needs = (wsId: string) => {
    const d = data[wsId];
    if (!d) return 0;
    const ids = new Set(d.columns.filter((c) => c.role === "needs-you").map((c) => c.id));
    return d.cards.filter((c) => ids.has(c.column_id)).length;
  };
  const drop = (onto: string) => {
    const from = dragId.current;
    dragId.current = null;
    setDrag(null);
    if (!from || from === onto) return;
    const to = visible.findIndex((w) => w.id === onto);
    if (to >= 0) void actions.moveWorkspace(from, to);
  };
  const deletingWs = visible.find((w) => w.id === deleting);
  return (
    <header className="top">
      <button className="ibtn only-narrow" onClick={onTogglePlanner} aria-label="Open the planner chat"><Icon name="chat" /></button>
      <div
        className="wsbar"
        role="tablist"
        aria-label="Workspaces"
        data-zone="workspaces"
        onKeyDown={(e) => {
          // ← and → move between the workspaces; ↩ or Space opens the focused one.
          if (e.target instanceof HTMLInputElement) return;
          const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
          if (!d || e.metaKey || e.altKey || e.ctrlKey || e.shiftKey) return;
          const tabs = Array.from(e.currentTarget.querySelectorAll<HTMLElement>("button.ws"));
          const at = tabs.indexOf(document.activeElement as HTMLElement);
          if (at < 0) return;
          e.preventDefault();
          tabs[(at + d + tabs.length) % tabs.length]?.focus();
        }}
      >
        {visible.map((w, i) => {
          const k = needs(w.id);
          if (editing === w.id) {
            return (
              <span key={w.id} className={"ws editing" + (w.id === activeId ? " on" : "")} data-ws-id={w.id}>
                <span className="dot" style={{ background: w.color }} />
                <RenameField
                  ws={w}
                  onSave={(name) => void actions.renameWorkspace(w.id, name)}
                  onDone={() => {
                    setEditing(null);
                    refocus(w.id);
                  }}
                />
              </span>
            );
          }
          const cls = "ws" + (w.id === activeId ? " on" : "") + (drag?.id === w.id ? " dragging" : "") + (drag && drag.over === w.id && drag.id !== w.id ? " dropto" : "");
          return (
            <button
              key={w.id}
              role="tab"
              aria-selected={w.id === activeId}
              className={cls}
              data-ws-id={w.id}
              title={i < 9 ? withChord(w.name, `workspace.${i + 1}`) : w.name}
              draggable
              onClick={() => void actions.switchWorkspace(w.id)}
              onDoubleClick={() => setEditing(w.id)}
              onContextMenu={(e) => {
                e.preventDefault();
                setMenu({ id: w.id, x: e.clientX, y: e.clientY });
              }}
              onDragStart={(e) => {
                dragId.current = w.id;
                e.dataTransfer?.setData?.("text/plain", w.name);
                if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
                setDrag({ id: w.id, over: null });
              }}
              onDragOver={(e) => {
                if (!dragId.current) return;
                e.preventDefault();
                if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
                if (drag?.over !== w.id) setDrag({ id: dragId.current, over: w.id });
              }}
              onDrop={(e) => {
                e.preventDefault();
                drop(w.id);
              }}
              onDragEnd={() => {
                dragId.current = null;
                setDrag(null);
              }}
            >
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
      <button className="ibtn" onClick={onFeedback} aria-label="Send feedback" title="Send feedback"><Icon name="bubble" /></button>
      <button className="ibtn" onClick={onPalette} aria-label="Command palette" title={withChord("Command palette", "palette.toggle")}><Icon name="search" /></button>
      <button className="ibtn" onClick={() => void themeActions.toggleMode()} aria-label="Switch light or dark" title="Switch light or dark"><Icon name="moon" /></button>
      <button className="ibtn" onClick={onSettings} aria-label="Settings" title={withChord("Settings", "settings.open")}><Icon name="gear" /></button>
      {menu ? <TabMenu board={board} at={menu} onClose={closeMenu} onRename={() => setEditing(menu.id)} onDelete={() => setDeleting(menu.id)} /> : null}
      {deletingWs ? <DeleteWorkspace board={board} ws={deletingWs} onClose={() => setDeleting(null)} /> : null}
    </header>
  );
}
