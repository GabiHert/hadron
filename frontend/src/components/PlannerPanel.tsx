import * as api from "../api";
import type { Board } from "../state/useBoard";
import { useSessionStatus } from "../state/useSessionStatus";
import { useChannel } from "../state/useChannel";
import { ChatThread } from "./Chat";
import { Composer } from "./Composer";
import { SessionTerminal } from "./SessionTerminal";
import { useState } from "react";
import { AdoptPlanner } from "./AdoptPlanner";
import { Icon } from "./ui";
import { ModelOptions, useModels } from "./ModelsTab";

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
  const { info: modelInfo, reload: reloadModels } = useModels(wsId, actions.toast);
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
              <label>Planner model
                <select className="in" aria-label="Planner model" value={modelInfo?.planner_model ?? ""} onChange={(e) => void actions.run(() => api.planner_set_model({ workspace_id: ws.id, model: e.target.value })).then((out) => { if (out) { actions.toast(out.note); void reloadModels(); } })}>
                  <option value="">{`Use default (${modelInfo?.default_model || "Claude's own"})`}</option>
                  <ModelOptions models={modelInfo?.models ?? []} current={modelInfo?.planner_model ?? ""} />
                </select>
              </label>
              <button className="btn sm" onClick={() => { setMenu(false); setAdoptOpen(true); }}>Adopt planner conversation</button>
            </div>
          ) : null}
        </div>
        <button className="ibtn mclose" onClick={closeMobile} aria-label="Close the planner"><Icon name="close" /></button>
      </div>
      {view === "chat" ? (
        <ChatView board={board} running={status ? status.running : undefined} />
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

/** The planner's conversation: your messages, its replies, and the reports it forwards. */
export function ChatView({ board, running }: { board: Board; running?: boolean }) {
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
        onError={actions.toast}
        target={{ workspace_id: ws.id, planner: true }}
        cards={board.cards}
        columns={board.columns}
        onCommand={ch.command}
        running={running}
        onStart={() => actions.run(() => api.planner_start({ workspace_id: ws.id }))}
        sessionName="The planner"
        suggestions={ch.loaded && ch.messages.length === 0 ? ["What needs me right now?", "Start a session for a new card"] : undefined}
      />
    </>
  );
}
