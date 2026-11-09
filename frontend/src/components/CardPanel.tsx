import { useEffect, useState } from "react";
import * as api from "../api";
import type { Board } from "../state/useBoard";
import { useSessionStatus } from "../state/useSessionStatus";
import { useChannel } from "../state/useChannel";
import { ChatThread } from "./Chat";
import { Composer } from "./Composer";
import { SessionTerminal } from "./SessionTerminal";
import { LinksTab, useLinks } from "./LinksTab";
import { Icon } from "./ui";

export type CardTab = "conv" | "links" | "ctx";

interface Props {
  board: Board;
  view: "chat" | "terminal";
  setView: (v: "chat" | "terminal") => void;
  tab: CardTab;
  setTab: (t: CardTab) => void;
  mobileOpen: boolean;
  closeMobile: () => void;
}

const LIVE: Record<string, [string, string]> = { working: ["ok", "working"], idle: ["", "idle"], waiting: ["warn", "waiting"], ended: ["", "ended"] };

export function CardPanel({ board, view, setView, tab, setTab, mobileOpen, closeMobile }: Props) {
  const { ws, selected: c, columns, actions } = board;
  const [menu, setMenu] = useState(false);
  const [nick, setNick] = useState("");
  const [assignee, setAssignee] = useState("");
  const cardId = c?.id ?? "";
  const status = useSessionStatus(() => (cardId ? api.session_status({ card: cardId }) : Promise.resolve(null)), [cardId]);
  const { links, reload: reloadLinks } = useLinks(cardId, actions.toast);
  useEffect(() => {
    setMenu(false);
    setNick(c?.nick ?? "");
    setAssignee(c?.assignee ?? "");
  }, [c?.id, c?.nick, c?.assignee]);
  if (!ws) return null;
  const isFull = ws.ui.full === "card";
  if (ws.ui.collapsed?.card && !mobileOpen && !isFull)
    return (
      <nav className="rail r" aria-label="Card panel, collapsed">
        <button className="ibtn" onClick={() => void actions.setCollapsed("card", false)} aria-label="Expand the card panel"><Icon name="back" /></button>
        <button className="vl" onClick={() => void actions.setCollapsed("card", false)}>{c ? <span className={`dot ${c.liveness}`} /> : null}{c ? c.nick : "Card"}</button>
      </nav>
    );
  if (!c)
    return (
      <aside className={"cpanel" + (mobileOpen ? " open" : "")}>
        <div className="empty" style={{ margin: "auto" }}>Pick a card to open its chat and terminal.</div>
      </aside>
    );
  const col = columns.find((x) => x.id === c.column_id);
  const openQ = board.openQuestions.find((q) => q.card_id === c.id);
  const live = LIVE[c.liveness] ?? ["", c.liveness];
  const claudeId = status?.session?.claude_session_id ?? "";
  const canResume = !!claudeId && !status?.running;
  return (
    <aside className={"cpanel" + (mobileOpen ? " open" : "") + (isFull ? " full" : "")} aria-label={`Card ${c.nick}`}>
      <div className="cph">
        <div className="l1">
          <button className="ibtn mclose" onClick={closeMobile} aria-label="Back to the board"><Icon name="back" /></button>
          {isFull ? null : <button className="ibtn hide-narrow" style={{ width: 26, height: 26 }} onClick={() => void actions.setCollapsed("card", true)} aria-label="Collapse the card panel"><Icon name="fwd" size={14} /></button>}
          <button className="ibtn hide-narrow" style={{ width: 26, height: 26 }} onClick={() => void actions.setFullscreen(isFull ? "" : "card")} aria-label={isFull ? "Exit full screen" : "Full screen"} title={isFull ? "Exit full screen (Esc, or Shift+Esc in the terminal)" : "Full screen"}>
            <Icon name={isFull ? "shrink" : "expand"} size={14} />
          </button>
          {c.key ? <span className="mono" style={{ fontSize: 12, color: "var(--fg3)" }}>{c.key}</span> : null}
          <span className="pill">{col?.name ?? ""}</span>
          <span className={"pill " + live[0]}><span className={`dot ${c.liveness}`} />{live[1]}</span>
          {canResume && view === "chat" ? (
            <button className="btn sm" aria-label="Resume the Claude conversation" title={`claude --resume ${claudeId.slice(0, 8)}`} onClick={() => void actions.run(() => api.session_start({ card: c.id }))}>Resume</button>
          ) : null}
          <div className="spacer" />
          <div style={{ position: "relative" }}>
            <button className="ibtn" style={{ width: 26, height: 26 }} onClick={() => setMenu(!menu)} aria-label="Card actions" aria-expanded={menu}><Icon name="dots" size={14} /></button>
            {menu ? (
              <div className="menu">
                <label>Nickname<input className="in" value={nick} onChange={(e) => setNick(e.target.value)} id="card-nick" /></label>
                <label>Assignee<input className="in" value={assignee} onChange={(e) => setAssignee(e.target.value)} id="card-assignee" /></label>
                <button
                  className="btn sm pri"
                  onClick={() => {
                    const patch: { nick?: string; assignee?: string } = {};
                    if (nick.trim() && nick.trim() !== c.nick) patch.nick = nick.trim();
                    if (assignee.trim() && assignee.trim() !== c.assignee) patch.assignee = assignee.trim();
                    if (Object.keys(patch).length) void actions.updateCard(c.id, patch);
                    setMenu(false);
                  }}
                >Save</button>
                <button className="btn sm" onClick={() => { void actions.archiveCard(c.id); setMenu(false); }}>Archive card</button>
                <button className="btn sm danger" onClick={() => { if (window.confirm(`Delete ${c.nick}? Its sessions, questions and links go with it. This cannot be undone.`)) void actions.deleteCard(c.id); setMenu(false); }}>Delete card</button>
              </div>
            ) : null}
          </div>
          <div className="seg" role="tablist" aria-label="Card view">
            <button className={view === "chat" ? "on" : ""} onClick={() => setView("chat")}><Icon name="chat" size={13} />Chat</button>
            <button className={view === "terminal" ? "on" : ""} onClick={() => setView("terminal")}><Icon name="term" size={13} />Terminal</button>
          </div>
        </div>
        <h2>{c.nick}</h2>
        {view === "chat" ? (
          <div className="tabs" role="tablist">
            <button className={"tab" + (tab === "conv" ? " on" : "")} role="tab" onClick={() => setTab("conv")}>
              Conversation{openQ ? <> <span className={"pill " + (openQ.kind === "approval" ? "bad" : "warn")} style={{ fontSize: 10.5 }}>{`Q-${String(openQ.seq).padStart(2, "0")}`}</span></> : null}
            </button>
            <button className={"tab" + (tab === "links" ? " on" : "")} role="tab" onClick={() => setTab("links")}>{`Links · ${links.length}`}</button>
            <button className={"tab" + (tab === "ctx" ? " on" : "")} role="tab" onClick={() => setTab("ctx")}>Context</button>
          </div>
        ) : (
          <div style={{ height: 8 }} />
        )}
      </div>
      {view === "terminal" ? (
        <SessionTerminal
          label={c.nick}
          status={status}
          foot={`claude · ${c.nick} · ${ws.root}`}
          banner={
            <div className="tbanner">
              <span style={{ flex: 1 }}>Direct to the session. The planner sees this only through the next report.</span>
              <button className="btn sm" onClick={() => setView("chat")}>Back to chat</button>
            </div>
          }
          onStart={() => void actions.run(() => api.session_start({ card: c.id }))}
          startLabel={canResume ? "Resume" : undefined}
          startTitle={canResume ? `claude --resume ${claudeId.slice(0, 8)}` : undefined}
          onRestart={() => void actions.run(() => api.session_restart({ card: c.id }))}
          onShiftEscape={isFull ? () => void actions.setFullscreen("") : undefined}
        />
      ) : tab === "conv" ? (
        <CardConversation board={board} />
      ) : tab === "links" ? (
        <LinksTab board={board} links={links} reload={reloadLinks} />
      ) : (
        <div className="cbody">
          <div className="box">
            <div className="fg">
              <label>Session</label><span className="mono" style={{ fontSize: 12 }}>{status?.session ? `${status.session.command} · ${status.running ? c.liveness : "ended"}` : "not started"} · {ws.root}</span>
              <label>Assignee</label><span>{c.assignee}</span>
              <label>Column</label><span>{col ? `${col.name} (${col.role || "no role"})` : ""}</span>
              <label>Work items</label><span>none yet</span>
              <label>Answered</label><span>none</span>
            </div>
          </div>
          <p className="hint">Context comes from the workspace's adapter. A generic workspace shows what the session reported.</p>
        </div>
      )}
    </aside>
  );
}

/** A card's conversation: what you said, what the planner relayed, the session's reports and its questions. */
function CardConversation({ board }: { board: Board }) {
  const { ws, selected: c, actions } = board;
  const ch = useChannel(ws?.id ?? "", c ? `card:${c.id}` : "", c?.id ?? null, actions.toast);
  if (!ws || !c) return null;
  const tag = `[${c.key ? c.key + " " : ""}${c.nick}]`;
  return (
    <>
      <ChatThread
        className="cbody"
        workspaceId={ws.id}
        onError={actions.toast}
        scope={`card:${c.id}`}
        messages={ch.messages}
        questions={ch.questions}
        onAnswer={ch.answer}
        empty={ch.loaded ? <div className="empty">Nothing yet. Reports and questions from {c.nick} appear here, and what you write goes to the planner.</div> : null}
      />
      <Composer id={`card-msg-${c.id}`} label="Message about this card" placeholder={`Goes to the planner tagged ${tag}; it relays to the session.`} onSend={ch.send} />
    </>
  );
}
