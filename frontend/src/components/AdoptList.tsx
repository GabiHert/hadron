import { useEffect, useState } from "react";
import * as api from "../api";
import type { Adoptable } from "../api";
import { hm, tildePath } from "../lib";

export const ADOPT_WARNING = "Exit that conversation in its terminal first. Two processes resuming the same Claude Code conversation corrupt it.";

/** The first up-to-3 words of a message, for a nickname suggestion. */
export function nickFrom(msg: string): string {
  const words = msg.toLowerCase().match(/[a-z0-9]+/g) ?? [];
  return words.slice(0, 3).join("-");
}

function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const mins = Math.round((Date.now() - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  if (mins < 24 * 60) return hm(iso);
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

interface Props {
  workspaceId: string;
  root: string;
  picked: string;
  onPick: (a: Adoptable) => void;
  onError: (text: string) => void;
}

/**
 * The warning, a folder to look in (the workspace root by default, or every folder), and the
 * picker of Claude Code conversations Hadron could take over. Each conversation carries the folder
 * it was run in, which the adopt call passes on as cwd.
 */
export function AdoptList({ workspaceId, root, picked, onPick, onError }: Props) {
  const [list, setList] = useState<Adoptable[] | null>(null);
  const [draft, setDraft] = useState(root);
  const [folder, setFolder] = useState(root);
  const [all, setAll] = useState(false);
  useEffect(() => {
    setDraft(root);
    setFolder(root);
  }, [root]);
  useEffect(() => {
    let alive = true;
    setList(null);
    const args = all ? { workspace_id: workspaceId, all: true } : folder.trim() && folder.trim() !== root ? { workspace_id: workspaceId, cwd: folder.trim() } : { workspace_id: workspaceId };
    api
      .session_adoptable_list(args)
      .then((l) => { if (alive) setList(l); })
      .catch((e) => { if (alive) { setList([]); onError(e instanceof Error ? e.message : String(e)); } });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceId, folder, all, root]);
  const look = () => setFolder(draft.trim() || root);
  return (
    <>
      <div className="box" role="note" style={{ margin: 0 }}>{ADOPT_WARNING}</div>
      <div className="field">
        <span>Folder the conversation was run in</span>
        <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
          <input
            className="in mono"
            aria-label="Conversation folder"
            style={{ flex: 1, minWidth: 0 }}
            value={draft}
            disabled={all}
            placeholder={root}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); look(); } }}
          />
          <button className="btn" disabled={all} onClick={look}>Show</button>
          <label style={{ display: "flex", gap: 4, alignItems: "center", whiteSpace: "nowrap" }}>
            <input type="checkbox" aria-label="All folders" checked={all} onChange={(e) => setAll(e.target.checked)} /> All folders
          </label>
        </div>
      </div>
      {list === null ? (
        <p className="hint" role="status">Looking for conversations…</p>
      ) : list.length === 0 ? (
        <p className="hint">{all ? "No Claude Code conversations were found in any folder." : `No Claude Code conversations were found for ${folder}.`}</p>
      ) : (
        <div className="scroll" role="listbox" aria-label="Claude Code conversations" style={{ maxHeight: 280, display: "flex", flexDirection: "column", gap: 6 }}>
          {list.map((a) => (
            <button
              key={a.claude_session_id}
              role="option"
              aria-selected={picked === a.claude_session_id}
              className={"btn" + (picked === a.claude_session_id ? " pri" : "")}
              disabled={a.already_in_hadron}
              title={a.already_in_hadron ? a.held_by : a.cwd}
              onClick={() => onPick(a)}
              style={{ textAlign: "left", display: "flex", gap: 8, alignItems: "center", height: "auto", padding: "6px 10px" }}
            >
              <span style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}>
                <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{a.first_user_message || "(no message)"}</span>
                {all && a.cwd ? <span className="hint mono" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{tildePath(a.cwd)}</span> : null}
              </span>
              {a.already_in_hadron ? <span className="pill">in Hadron</span> : null}
              <span className="hint">{when(a.last_activity)} · {Math.max(1, Math.round(a.size / 1024))} KB</span>
            </button>
          ))}
        </div>
      )}
    </>
  );
}
