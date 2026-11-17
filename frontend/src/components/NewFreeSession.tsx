import { useEffect, useState } from "react";
import * as api from "../api";
import type { FreeKind } from "../api";
import type { Board } from "../state/useBoard";
import { startFree } from "../state/freeSessions";
import { FolderInput, ModalHead, Scrim } from "./Modals";

/** "New session in a folder": a shell or claude, a folder picked in Finder, an optional name and workspace tag. */
export function NewFreeSession({ board, onClose, onStarted }: { board: Board; onClose: () => void; onStarted: () => void }) {
  const [kind, setKind] = useState<FreeKind>("shell");
  const [cwd, setCwd] = useState("");
  const [name, setName] = useState("");
  const [tag, setTag] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    Promise.resolve(api.free_session_list())
      .then((l) => live && setCwd((c) => c || l?.default_cwd || ""))
      .catch(() => undefined);
    return () => void (live = false);
  }, []);
  const submit = async () => {
    setBusy(true);
    try {
      await startFree({ kind, cwd: cwd.trim() || undefined, name: name.trim() || undefined, workspace_id: tag || undefined });
      onStarted();
      onClose();
    } catch (e) {
      board.actions.toast(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Scrim onClose={onClose}>
      <form
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="nf-t"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <ModalHead id="nf-t" title="New session" onClose={onClose} />
        <div className="mb">
          <div className="field">
            <span>Run</span>
            <div className="seg" role="radiogroup" aria-label="What to run">
              <button type="button" role="radio" aria-checked={kind === "shell"} className={kind === "shell" ? "on" : ""} onClick={() => setKind("shell")}>Terminal (your login shell)</button>
              <button type="button" role="radio" aria-checked={kind === "claude"} className={kind === "claude" ? "on" : ""} onClick={() => setKind("claude")}>claude</button>
            </div>
          </div>
          <div className="field">
            <span id="nf-cwd-l">Folder</span>
            <FolderInput id="nf-cwd" labelledBy="nf-cwd-l" value={cwd} onChange={setCwd} onError={board.actions.toast} />
          </div>
          <label className="field"><span>Name (optional)</span><input className="in" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. api server" /></label>
          <label className="field">
            <span>Workspace tag (optional)</span>
            <select className="in" value={tag} onChange={(e) => setTag(e.target.value)}>
              <option value="">None: it is global</option>
              {board.visible.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
          </label>
          <p className="hint" style={{ margin: 0 }}>It belongs to no card and keeps running in every workspace. It comes back after Hadron restarts: a shell in its last folder, claude resuming its conversation.</p>
        </div>
        <div className="mf">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn pri" disabled={busy}>Start</button>
        </div>
      </form>
    </Scrim>
  );
}
