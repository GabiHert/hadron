import * as api from "../api";
import type { Board } from "../state/useBoard";
import { useSessionStatus } from "../state/useSessionStatus";
import { useChannel } from "../state/useChannel";
import { ChatThread } from "./Chat";
import { Composer } from "./Composer";
import { SessionTerminal } from "./SessionTerminal";
import { useState } from "react";
import type { Adoptable } from "../api";
import { AdoptList } from "./AdoptList";
import { ModalHead, Scrim } from "./Modals";
import { Icon } from "./ui";

interface Props {
  board: Board;
  view: "chat" | "terminal";
  setView: (v: "chat" | "terminal") => void;
  mobileOpen: boolean;
  closeMobile: () => void;
}

/** The planner's panel: its chat or its terminal. */
export function PlannerPanel({ board, view, setView, mobileOpen, closeMobile }: Props) {
  const { ws, actions } = board;
  const wsId = ws?.id ?? "";
  const [menu, setMenu] = useState(false);
  const [adoptOpen, setAdoptOpen] = useState(false);
  const status = useSessionStatus(() => (wsId ? api.planner_get({ workspace_id: wsId }) : Promise.resolve(null)), [wsId]);
  if (!ws) return null;
  // none (never started) reads as idle: there is nothing to pulse about.
  const dot = status && status.liveness !== "none" ? status.liveness : "idle";
  const isFull = ws.ui.full === "planner";
  const collapsed = ws.ui.collapsed?.planner && !mobileOpen && !isFull;
  if (collapsed)
    return (
      <nav className="rail" aria-label="Planner, collapsed">
        <button className="ibtn" onClick={() => void actions.setCollapsed("planner", false)} aria-label="Expand the planner"><Icon name="fwd" /></button>
        <button className="vl" onClick={() => void actions.setCollapsed("planner", false)}><span className={`dot ${dot}`} />Planner</button>
      </nav>
    );
  return (
    <aside className={"ppanel" + (mobileOpen ? " open" : "") + (isFull ? " full" : "")} aria-label="Planner">
      <div className="phead">
        <span className={`dot ${dot}${dot === "working" ? " pulse" : ""}`} title={`Planner ${dot}`} />
        <h2>Planner{isFull ? ` · ${ws.name}` : ""}</h2>
        <div className="seg" role="tablist" aria-label="Planner view">
          <button className={view === "chat" ? "on" : ""} onClick={() => setView("chat")}><Icon name="chat" size={13} />Chat</button>
          <button className={view === "terminal" ? "on" : ""} onClick={() => setView("terminal")}><Icon name="term" size={13} />Terminal</button>
        </div>
        <button className="ibtn hide-narrow" onClick={() => void actions.setFullscreen(isFull ? "" : "planner")} aria-label={isFull ? "Exit full screen" : "Full screen"} title={isFull ? "Exit full screen (Esc, or Shift+Esc in the terminal)" : "Full screen"}>
          <Icon name={isFull ? "shrink" : "expand"} />
        </button>
        {isFull ? null : <button className="ibtn hide-narrow" onClick={() => void actions.setCollapsed("planner", true)} aria-label="Collapse the planner"><Icon name="back" /></button>}
        <div style={{ position: "relative" }}>
          <button className="ibtn" onClick={() => setMenu(!menu)} aria-label="Planner actions" aria-expanded={menu}><Icon name="dots" size={14} /></button>
          {menu ? (
            <div className="menu">
              <button className="btn sm" onClick={() => { setMenu(false); setAdoptOpen(true); }}>Adopt planner conversation</button>
            </div>
          ) : null}
        </div>
        <button className="ibtn mclose" onClick={closeMobile} aria-label="Close the planner"><Icon name="close" /></button>
      </div>
      {view === "chat" ? (
        <ChatView board={board} />
      ) : (
        <SessionTerminal
          label="planner"
          status={status}
          foot="claude · planner · typing here skips the chat"
          onStart={() => void actions.run(() => api.planner_start({ workspace_id: ws.id }))}
          onRestart={() => void actions.run(() => api.planner_restart({ workspace_id: ws.id }))}
          onShiftEscape={isFull ? () => void actions.setFullscreen("") : undefined}
        />
      )}
      {adoptOpen ? <AdoptPlanner board={board} onClose={() => setAdoptOpen(false)} /> : null}
    </aside>
  );
}

function AdoptPlanner({ board, onClose }: { board: Board; onClose: () => void }) {
  const { ws, actions } = board;
  const [picked, setPicked] = useState<Adoptable | null>(null);
  const [busy, setBusy] = useState(false);
  if (!ws) return null;
  const adopt = async () => {
    if (!picked) return;
    setBusy(true);
    const out = await actions.run(() => api.planner_adopt({ workspace_id: ws.id, claude_session_id: picked.claude_session_id }));
    setBusy(false);
    if (!out) return;
    actions.toast(out.warning);
    onClose();
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="ap-t">
        <ModalHead id="ap-t" title="Adopt planner conversation" onClose={onClose} />
        <div className="mb">
          <AdoptList workspaceId={ws.id} root={ws.root} picked={picked?.claude_session_id ?? ""} onPick={setPicked} onError={actions.toast} />
        </div>
        <div className="mf">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn pri" disabled={!picked || busy} onClick={() => void adopt()}>Adopt as planner</button>
        </div>
      </div>
    </Scrim>
  );
}

/** The planner's conversation: your messages, its replies, and the reports it forwards. */
export function ChatView({ board }: { board: Board }) {
  const { ws, actions } = board;
  const ch = useChannel(ws?.id ?? "", "planner", null, actions.toast);
  if (!ws) return null;
  return (
    <>
      <ChatThread
        className="chat"
        workspaceId={ws.id}
        onError={actions.toast}
        scope={`planner:${ws.id}`}
        messages={ch.messages}
        questions={ch.questions}
        onAnswer={ch.answer}
        empty={ch.loaded ? <div className="empty">The planner runs every session in {ws.name}. Ask it to start one.</div> : null}
      />
      <Composer
        id="planner-msg"
        label="Message the planner"
        placeholder="Goes to the planner, tagged [planner]."
        onSend={ch.send}
        suggestions={ch.loaded && ch.messages.length === 0 ? ["What needs me right now?", "Start a session for a new card"] : undefined}
      />
    </>
  );
}
