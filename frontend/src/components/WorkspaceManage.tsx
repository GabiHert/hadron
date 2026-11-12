import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { Workspace, WorkspaceDeleteOut } from "../api";
import type { Board } from "../state/useBoard";
import { ModalHead, Scrim } from "./Modals";

const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`;

/** The sentence that says what goes with a workspace when it is deleted. */
export function deleteSummary(c: Pick<WorkspaceDeleteOut, "cards" | "sessions" | "running_sessions" | "messages" | "questions">): string {
  const parts = [plural(c.cards, "card"), plural(c.sessions, "session"), plural(c.messages, "chat message"), plural(c.questions, "question")];
  return `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

/**
 * Archive or delete a workspace. Archive is the safe choice and has the focus; delete says what
 * goes with the workspace, stops its running sessions and cannot be undone. Files are never touched.
 */
export function DeleteWorkspace({ board, ws, onClose }: { board: Board; ws: Workspace; onClose: () => void }) {
  const [info, setInfo] = useState<WorkspaceDeleteOut | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const safe = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    let live = true;
    api.workspace_delete({ workspace_id: ws.id, dry_run: true }).then(
      (out) => live && setInfo(out),
      (e: unknown) => live && setError(e instanceof Error ? e.message : String(e)),
    );
    return () => {
      live = false;
    };
  }, [ws.id]);
  useEffect(() => {
    if (info) safe.current?.focus();
  }, [info]);
  const act = async (what: "archive" | "delete") => {
    setBusy(true);
    const out = what === "archive" ? await board.actions.archiveWorkspace(ws.id) : await board.actions.deleteWorkspace(ws.id);
    setBusy(false);
    if (out) onClose();
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal confirm" role="alertdialog" aria-modal="true" aria-labelledby="wd-t" aria-describedby="wd-d">
        <ModalHead id="wd-t" title={`Delete ${ws.name}?`} onClose={onClose} />
        <div className="mb" id="wd-d">
          {error ? <p style={{ margin: 0 }} role="alert">{error}</p> : null}
          {!error && !info ? <p style={{ margin: 0 }}>Counting what is in it…</p> : null}
          {info ? (
            <>
              <p style={{ margin: "0 0 8px" }}>
                Deleting removes its {deleteSummary(info)}, with its columns, rules and dashboard. This cannot be undone.
              </p>
              {info.running_sessions ? <p style={{ margin: "0 0 8px" }}>{plural(info.running_sessions, "running session")} will be stopped first.</p> : null}
              <p style={{ margin: "0 0 8px" }}>
                Files in its root folder <span className="mono">{ws.root}</span> and worktrees on disk are not touched.
              </p>
              <p style={{ margin: 0 }}>Archive it instead to hide it and stop its sessions while keeping everything.</p>
            </>
          ) : null}
        </div>
        <div className="mf">
          <button className="btn" onClick={onClose}>Cancel</button>
          {info ? (
            <>
              <button ref={safe} className="btn pri" disabled={busy} onClick={() => void act("archive")}>Archive instead</button>
              <button className="btn danger" disabled={busy} onClick={() => void act("delete")}>Delete workspace</button>
            </>
          ) : null}
        </div>
      </div>
    </Scrim>
  );
}

/** A workspace's name, edited in place in its tab. Enter or leaving the field saves; Esc cancels. */
export function RenameField({ ws, onDone, onSave }: { ws: Workspace; onDone: () => void; onSave: (name: string) => void }) {
  const [name, setName] = useState(ws.name);
  const done = useRef(false);
  const finish = (save: boolean) => {
    if (done.current) return;
    done.current = true;
    const v = name.trim();
    if (save && v && v !== ws.name) onSave(v);
    onDone();
  };
  return (
    <input
      className="in wsname"
      aria-label={`Rename ${ws.name}`}
      autoFocus
      maxLength={60}
      size={Math.max(4, name.length)}
      value={name}
      onFocus={(e) => e.currentTarget.select()}
      onChange={(e) => setName(e.target.value)}
      onBlur={() => finish(true)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          finish(e.key === "Enter");
        }
      }}
    />
  );
}

export interface TabMenuAt {
  id: string;
  x: number;
  y: number;
}

/** The right-click menu of a workspace tab: rename, move, archive or delete, and restore an archived one. */
export function TabMenu({ board, at, onRename, onDelete, onClose }: { board: Board; at: TabMenuAt; onRename: () => void; onDelete: () => void; onClose: (refocus: boolean) => void }) {
  const root = useRef<HTMLDivElement>(null);
  const [archived, setArchived] = useState<Workspace[]>([]);
  const ws = board.visible.find((w) => w.id === at.id);
  const i = board.visible.findIndex((w) => w.id === at.id);
  useEffect(() => {
    let live = true;
    api.workspace_list({ include_archived: true }).then((all) => live && setArchived(all.filter((w) => w.archived)), () => undefined);
    return () => {
      live = false;
    };
  }, []);
  useEffect(() => {
    root.current?.querySelector<HTMLButtonElement>('[role="menuitem"]:not([disabled])')?.focus();
    const away = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) onClose(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [onClose]);
  if (!ws) return null;
  const item = (label: string, run: () => void, disabled = false) => (
    <button type="button" role="menuitem" disabled={disabled} onClick={() => { onClose(false); run(); }}>
      {label}
    </button>
  );
  return (
    <div
      ref={root}
      className="wmenu-list wstabmenu"
      role="menu"
      aria-label={`Actions for ${ws.name}`}
      style={{ position: "fixed", left: at.x, top: at.y, zIndex: 50 }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          onClose(true);
          return;
        }
        if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
        e.preventDefault();
        e.stopPropagation();
        const items = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ?? []);
        const k = items.indexOf(document.activeElement as HTMLButtonElement);
        items[(k + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]?.focus();
      }}
    >
      {item("Rename", onRename)}
      {item("Move left", () => void board.actions.moveWorkspace(ws.id, i - 1), i <= 0)}
      {item("Move right", () => void board.actions.moveWorkspace(ws.id, i + 1), i >= board.visible.length - 1)}
      {item("Archive or delete…", onDelete, board.visible.length < 2)}
      {archived.map((w) => item(`Restore ${w.name}`, () => void board.actions.run(() => api.workspace_archive({ workspace_id: w.id, archived: false }))))}
    </div>
  );
}
