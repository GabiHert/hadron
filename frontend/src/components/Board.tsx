import { useEffect, useRef, useState } from "react";
import type { DragEvent } from "react";
import type { Card, Column, Question } from "../api";
import { ROLES, short } from "../lib";
import { effectiveModel } from "../state/useBoard";
import type { Board as BoardState } from "../state/useBoard";
import { useFilters } from "../state/useFilters";
import { Dashboard } from "./Dashboard";
import { FilterBar } from "./FilterBar";
import { Icon, closeOnEscape } from "./ui";
import { withChord } from "../keys";
import { attrEsc } from "../commands";
import { unseen } from "../state/foldSeen";

interface Props {
  board: BoardState;
  onNewSession: () => void;
  onNewWorkspace?: () => void;
}

export function Board({ board, onNewSession, onNewWorkspace }: Props) {
  const { ws, columns, cards, selected, actions } = board;
  const filters = useFilters(board);
  const [editId, setEditId] = useState<string | null>(null);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [dragId, setDragId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);

  if (!ws) {
    if (board.loadError)
      return (
        <main className="board">
          <div className="empty" role="alert" style={{ margin: "auto", display: "flex", flexDirection: "column", gap: 10, alignItems: "center" }}>
            <span>Hadron could not load its data: {board.loadError}</span>
            <button className="btn" onClick={() => void actions.refetch()}>Try again</button>
          </div>
        </main>
      );
    if (!board.ready) return <main className="board"><div className="empty" role="status" style={{ margin: "auto" }}>Loading your workspaces…</div></main>;
    return (
      <main className="board">
        <div className="empty" style={{ margin: "auto", display: "flex", flexDirection: "column", gap: 10, alignItems: "center" }}>
          <span>Create a workspace to start. It gets a board, a planner and a place for your sessions to work.</span>
          {onNewWorkspace ? <button className="btn pri" onClick={onNewWorkspace}><Icon name="plus" size={13} />New workspace</button> : null}
        </div>
      </main>
    );
  }
  const dash = ws.ui.main_view === "dashboard";
  const match = (c: Card) => filters.visible(c);

  const viewSeg = (
    <div className="seg mview" role="tablist" aria-label="Main view">
      <button className={!dash ? "on" : ""} role="tab" aria-selected={!dash} title={withChord("Board", "view.dashboard")} onClick={() => void actions.setMainView("board")}>
        <Icon name="board" size={13} />Board
      </button>
      <button className={dash ? "on" : ""} role="tab" aria-selected={dash} title={withChord("Dashboard", "view.dashboard")} onClick={() => void actions.setMainView("dashboard")}>
        <Icon name="dash" size={13} />Dashboard
      </button>
    </div>
  );
  const newBtn = (
    <button className="btn" onClick={onNewSession} title={withChord("New session", "card.new")}>
      <Icon name="plus" size={13} />New session
    </button>
  );

  if (dash)
    return (
      <main className="board" data-zone="dashboard" tabIndex={-1} aria-label="Dashboard">
        <div className="bhead">
          <div className="r1"><h1>{ws.name}</h1>{viewSeg}<div className="spacer" />{newBtn}</div>
        </div>
        <Dashboard board={board} />
      </main>
    );

  const addColumn = async () => {
    const col = await actions.addColumn();
    if (col) setEditId(col.id);
  };

  return (
    <main className="board" data-zone="board" tabIndex={-1} aria-label="Board">
      <div className="bhead">
        <div className="r1">
          <h1>{ws.name}</h1>
          {viewSeg}
          <span className="cap" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>each card is one Claude session · <kbd className="kbd" title="Keyboard shortcuts">?</kbd> for keys</span>
          <div className="spacer" />
          {newBtn}
        </div>
        <FilterBar board={board} filters={filters} />
      </div>
      <div className="cols">
        {columns.map((col, ci) => (
          <ColumnView
            key={col.id}
            col={col}
            index={ci}
            count={columns.length}
            all={cards.filter((c) => c.column_id === col.id)}
            shown={cards.filter((c) => c.column_id === col.id && match(c))}
            board={board}
            selectedId={selected?.id ?? null}
            editing={editId === col.id}
            setEditing={(on) => setEditId(on ? col.id : null)}
            menuOpen={menuId === col.id}
            setMenuOpen={(on) => setMenuId(on ? col.id : null)}
            dragId={dragId}
            setDragId={setDragId}
            over={overId === col.id}
            setOver={(on) => setOverId((o) => (on ? col.id : o === col.id ? null : o))}
          />
        ))}
        <button className="addcol" onClick={() => void addColumn()} title={withChord("Add a column", "column.new")}>
          <Icon name="plus" size={13} /> Column
        </button>
      </div>
    </main>
  );
}

interface ColProps {
  col: Column;
  index: number;
  count: number;
  all: Card[];
  shown: Card[];
  board: BoardState;
  selectedId: string | null;
  editing: boolean;
  setEditing: (on: boolean) => void;
  menuOpen: boolean;
  setMenuOpen: (on: boolean) => void;
  dragId: string | null;
  setDragId: (id: string | null) => void;
  over: boolean;
  setOver: (on: boolean) => void;
}

export function ColumnView(p: ColProps) {
  const { col, all, shown, board, selectedId, dragId } = p;
  const { actions } = board;
  const cancelled = useRef(false);
  const overWip = col.wip > 0 && all.length > col.wip;
  const pillCls = overWip ? " warn" : col.role === "needs-you" && all.length ? " bad" : "";

  if (col.fold) {
    const fresh = unseen(col.id, all.map((c) => c.id));
    const asking = all.filter((c) => board.openQuestions.some((q) => q.card_id === c.id)).length;
    const notes = [`${all.length} card${all.length === 1 ? "" : "s"}`, fresh.length ? `${fresh.length} new since you collapsed it` : "", asking ? `${asking} waiting on you` : ""].filter(Boolean).join(", ");
    return (
      <section
        className={"col folded" + (p.over ? " drop" : "")}
        aria-label={`Column ${col.name}, collapsed`}
        data-col-id={col.id}
        data-col-name={col.name}
        data-folded=""
        onDragOver={(e) => {
          e.preventDefault();
          p.setOver(true);
        }}
        onDragLeave={() => p.setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          const id = dragId;
          p.setDragId(null);
          p.setOver(false);
          if (id) void actions.dropCard(id, col.id);
        }}
      >
        <button
          className={"rail" + (col.role === "needs-you" && all.length ? " needs" : "")}
          onClick={() => void actions.collapseColumn(col.id, false)}
          aria-label={`Expand ${col.name}`}
          title={withChord(`Expand ${col.name}: ${notes}`, "column.collapse")}
        >
          <span className={"pill" + pillCls} data-testid={`count-${col.id}`}>{all.length}</span>
          {fresh.length ? <span className="rbadge" data-testid={`new-${col.id}`}>+{fresh.length}</span> : null}
          {asking ? <span className="rbadge bad" data-testid={`asking-${col.id}`}>?</span> : null}
          <span className="rname">{col.name}</span>
        </button>
      </section>
    );
  }

  const commit = (value: string) => {
    if (cancelled.current) return;
    const name = value.trim();
    p.setEditing(false);
    if (name && name !== col.name) void actions.renameColumn(col.id, name);
  };

  // Index of the drop among the column's cards without the dragged one, so it matches card_move.
  const dropOn = (e: DragEvent, target: Card) => {
    e.preventDefault();
    e.stopPropagation();
    const id = dragId;
    p.setDragId(null);
    p.setOver(false);
    if (!id || id === target.id) return;
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const before = !(e.clientY > rect.top + rect.height / 2);
    const rest = all.filter((c) => c.id !== id);
    const at = rest.findIndex((c) => c.id === target.id);
    void actions.dropCard(id, col.id, before ? at : at + 1);
  };

  return (
    <section
      className={"col" + (p.over ? " drop" : "")}
      aria-label={`Column ${col.name}`}
      data-col-id={col.id}
      data-col-name={col.name}
      onDragOver={(e) => {
        e.preventDefault();
        p.setOver(true);
      }}
      onDragLeave={() => p.setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        const id = dragId;
        p.setDragId(null);
        p.setOver(false);
        if (id) void actions.dropCard(id, col.id);
      }}
    >
      <div className="chead">
        {p.editing ? (
          <EditName
            name={col.name}
            id={col.id}
            cancelled={cancelled}
            onCommit={commit}
            onCancel={() => {
              cancelled.current = true;
              p.setEditing(false);
            }}
          />
        ) : (
          <button
            className={"cname" + (col.role === "needs-you" ? " needs" : "")}
            onClick={() => {
              cancelled.current = false;
              p.setEditing(true);
            }}
            title="Rename"
          >
            {col.name}
          </button>
        )}
        <span className={"pill" + pillCls} data-testid={`count-${col.id}`}>
          {all.length}
          {col.wip ? ` / ${col.wip}` : ""}
        </span>
        <div className="spacer" />
        <button className="ibtn" style={{ width: 24, height: 24 }} onClick={() => void actions.collapseColumn(col.id, true)} aria-label={`Collapse ${col.name}`} title={withChord("Collapse the column", "column.collapse")}>
          <Icon name="back" size={13} />
        </button>
        <button className="ibtn" style={{ width: 24, height: 24 }} onClick={() => p.setMenuOpen(!p.menuOpen)} aria-label={`Column options for ${col.name}`} aria-expanded={p.menuOpen}>
          <Icon name="dots" size={14} />
        </button>
        {p.menuOpen ? <ColumnMenu {...p} /> : null}
      </div>
      <div className="clist">
        {shown.map((c) => (
          <CardFace
            key={c.id}
            card={c}
            question={board.openQuestions.find((q) => q.card_id === c.id)}
            chips={board.chips[c.id]}
            model={effectiveModel(c, board.profiles, board.ws)}
            selected={selectedId === c.id}
            dragging={dragId === c.id}
            onOpen={() => board.actions.open(c.id)}
            onDragStart={() => p.setDragId(c.id)}
            onDragEnd={() => {
              p.setDragId(null);
              p.setOver(false);
            }}
            onDrop={(e) => dropOn(e, c)}
          />
        ))}
        {!shown.length ? <div className="hint" style={{ padding: "10px 6px", textAlign: "center" }}>{all.length ? "Hidden by the filter" : "Drop a card here"}</div> : null}
      </div>
    </section>
  );
}

function EditName({ name, id, cancelled, onCommit, onCancel }: { name: string; id: string; cancelled: { current: boolean }; onCommit: (v: string) => void; onCancel: () => void }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => ref.current?.focus(), []);
  return (
    <input
      ref={ref}
      className="in cname-in"
      defaultValue={name}
      aria-label="Column name"
      id={`colname-${id}`}
      onBlur={(e) => onCommit(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
        if (e.key === "Escape") onCancel();
      }}
      onFocus={() => {
        cancelled.current = false;
      }}
    />
  );
}

function ColumnMenu({ col, index, count, board, setMenuOpen }: ColProps) {
  const { actions } = board;
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => ref.current?.querySelector<HTMLElement>("input, select, button")?.focus(), []);
  const opener = { get current() { return document.querySelector<HTMLElement>(`[aria-label="Column options for ${attrEsc(col.name)}"]`); } };
  return (
    <div className="menu" ref={ref} onKeyDown={closeOnEscape(() => setMenuOpen(false), opener)}>
      <label>
        Name
        <input className="in" defaultValue={col.name} id={`menu-name-${col.id}`} onBlur={(e) => { const n = e.target.value.trim(); if (n && n !== col.name) void actions.renameColumn(col.id, n); }} />
      </label>
      <label>
        Role (rules use it, never the name)
        <select className="in" value={col.role} onChange={(e) => void actions.updateColumn(col.id, { role: e.target.value as Column["role"] })}>
          <option value="">No role</option>
          {ROLES.map((r) => <option key={r[0]} value={r[0]}>{r[1]}</option>)}
        </select>
      </label>
      <label>
        WIP limit (0 = none)
        <input className="in" type="number" min={0} max={50} value={col.wip} id={`menu-wip-${col.id}`} onChange={(e) => void actions.updateColumn(col.id, { wip: Math.max(0, parseInt(e.target.value || "0", 10)) })} />
      </label>
      <div className="inline">
        <button className="btn sm" onClick={() => void actions.moveColumn(col.id, -1)} disabled={index === 0}>← Move</button>
        <button className="btn sm" onClick={() => void actions.moveColumn(col.id, 1)} disabled={index === count - 1}>Move →</button>
        <button className="btn sm" onClick={() => { void actions.collapseColumn(col.id, true); setMenuOpen(false); }}>Collapse</button>
      </div>
      <button className="btn sm danger" onClick={() => { void actions.deleteColumn(col.id); setMenuOpen(false); }} disabled={count < 2}>Delete column</button>
    </div>
  );
}

interface FaceProps {
  card: Card;
  /** the card's open question, if it has one */
  question?: Question;
  /** linked-field chips (value shown, label in the tooltip) */
  chips?: { path: string; label: string; value: unknown }[];
  /** the model the card runs, shown as a small chip when one is set */
  model?: { id: string; source: string } | null;
  selected: boolean;
  dragging: boolean;
  onOpen: () => void;
  onDragStart: () => void;
  onDragEnd: () => void;
  onDrop: (e: DragEvent) => void;
}

export function CardFace({ card: c, question, chips, model, selected, dragging, onOpen, onDragStart, onDragEnd, onDrop }: FaceProps) {
  return (
    <button
      draggable
      className={"card" + (selected ? " sel" : "") + (question ? " ask" : "") + (dragging ? " dragging" : "")}
      onDragStart={(e) => {
        e.dataTransfer?.setData?.("text/plain", c.id);
        if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
        onDragStart();
      }}
      onDragEnd={onDragEnd}
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDrop}
      onClick={onOpen}
      aria-label={`Open ${c.nick}`}
      data-card-id={c.id}
      aria-current={selected ? "true" : undefined}
    >
      <span className="l1">
        <span className={`dot ${c.liveness}${c.liveness === "working" ? " pulse" : ""}`} title={c.liveness} />
        <span className="key">{c.key || `~${c.nick}`}</span>
        {question ? (
          <span className={"pill " + (question.kind === "approval" ? "bad" : "warn")}>{question.kind === "approval" ? "approval" : `Q-${String(question.seq).padStart(2, "0")}`}</span>
        ) : null}
        <span className="spacer" />
        {c.assignee && c.assignee !== "You" ? <span className="hint">{c.assignee}</span> : null}
      </span>
      <span className="nick">{c.nick}</span>
      {question ? <span className="q">{short(question.text, 90)}</span> : null}
      {c.liveness === "idle" || chips?.length || model ? (
        <div className="chips">
          {chips?.map((ch) => <span key={ch.path} className="pill" title={`${ch.label}: ${String(ch.value)}`}>{String(ch.value)}</span>)}
          {model ? <span className="pill" title={`Model: ${model.id} (${model.source})`}>{model.id}</span> : null}
          {c.liveness === "idle" ? <span className="pill">idle</span> : null}
        </div>
      ) : null}
    </button>
  );
}
