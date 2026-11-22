import { useEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, KeyboardEvent } from "react";
import * as api from "../api";
import type { BlockView, Card, GroupCardRow } from "../api";
import { focusEl, requestUI, whenReady } from "../commands";
import { edgeScroll, short } from "../lib";
import type { Board as BoardState } from "../state/useBoard";
import { Icon, closeOnEscape } from "./ui";

/** The drag payload of a card in a group. */
export const GROUP_DRAG = "application/x-hadron-group-card";

type Drag = { card: string; from: string };

const LIVE_LABEL: Record<string, string> = { working: "working", idle: "idle", waiting: "waiting", ended: "ended", asleep: "asleep" };

/** One card of a group as the dashboard shows it: the board's live card, plus the server's last line. */
export interface GroupItem {
  card: Card;
  row?: GroupCardRow;
  column: string;
  needsYou: boolean;
  question: string;
}

/** The group's cards in order, read live from the board; a card that left the board is skipped. */
export function groupItems(b: BlockView, board: BoardState): GroupItem[] {
  const rows = new Map((b.data?.rows ?? []).map((r) => [String(r.id), r as unknown as GroupCardRow]));
  const ids = b.source?.cards ?? [];
  const out: GroupItem[] = [];
  for (const id of ids) {
    const card = board.cards.find((c) => c.id === id);
    if (!card || card.archived) continue;
    const col = board.columns.find((c) => c.id === card.column_id);
    const q = board.openQuestions.find((x) => x.card_id === id);
    const row = rows.get(id);
    out.push({ card, row, column: col?.name ?? row?.column ?? "", needsYou: !!q || col?.role === "needs-you" || !!row?.needs_you, question: q?.text ?? row?.question ?? "" });
  }
  return out;
}

/** Where a drop lands: before the row at `index`, or at the end. In the group it came from, the card's own slot does not count. */
export function dropPosition(ids: string[], drag: Drag, groupId: string, index: number | undefined): number | undefined {
  if (index === undefined) return undefined;
  if (drag.from === groupId) {
    const at = ids.indexOf(drag.card);
    if (at >= 0 && at < index) return index - 1;
  }
  return index;
}

const readDrag = (e: DragEvent): Drag | null => {
  try {
    const raw = e.dataTransfer?.getData?.(GROUP_DRAG);
    if (!raw) return null;
    const d = JSON.parse(raw) as Drag;
    return typeof d.card === "string" && typeof d.from === "string" ? d : null;
  } catch {
    return null;
  }
};

const isGroupDrag = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes(GROUP_DRAG);

interface Props {
  b: BlockView;
  board: BoardState;
  /** every group on the dashboard, for "Move to" and "Copy to" */
  groups: { id: string; title: string }[];
  mutate: <T>(fn: () => Promise<T>) => Promise<T | undefined>;
  /** open the card picker on mount (a group just made) */
  startPicking?: boolean;
}

/** A card group block's body: the watched cards, live, with drag and drop between groups. */
export function GroupBody({ b, board, groups, mutate, startPicking }: Props) {
  const items = groupItems(b, board);
  const ids = items.map((x) => x.card.id);
  const [picking, setPicking] = useState(!!startPicking);
  const [over, setOver] = useState<number | "end" | null>(null);
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (startPicking) setPicking(true);
  }, [startPicking]);

  const focusRow = (id: string) => whenReady(() => document.querySelector<HTMLElement>(`[data-block="${b.id}"] [data-group-card="${id}"]`), (el) => focusEl(el));

  const move = async (card: string, from: string, to: string, position: number | undefined, copy: boolean) => {
    await mutate(() => api.card_group_move({ card, from, to, position, copy }));
  };

  const onDrop = (e: DragEvent, index: number | undefined) => {
    const d = readDrag(e);
    setOver(null);
    if (!d) return;
    e.preventDefault();
    e.stopPropagation();
    const copy = !!e.altKey && d.from !== b.id;
    const pos = dropPosition(ids, d, b.id, index);
    if (d.from === b.id && (pos === undefined ? ids.indexOf(d.card) === ids.length - 1 : ids.indexOf(d.card) === pos)) return;
    void move(d.card, d.from, b.id, pos, copy);
  };
  const onOver = (e: DragEvent, at: number | "end") => {
    if (!isGroupDrag(e)) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.dataTransfer) e.dataTransfer.dropEffect = e.altKey ? "copy" : "move";
    if (over !== at) setOver(at);
  };

  const remove = (id: string) => void mutate(() => api.card_group_remove({ group: b.id, cards: [id] }));
  const open = (id: string) => {
    board.actions.open(id);
    requestUI({ do: "card.chat" });
  };

  const onRowKey = (e: KeyboardEvent<HTMLElement>, id: string, i: number) => {
    const step = (d: number) => {
      const next = ids[i + d];
      if (next) focusRow(next);
    };
    if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
      const to = e.key === "ArrowUp" ? i - 1 : i + 1;
      if (to >= 0 && to < ids.length) void move(id, b.id, b.id, to, false).then(() => focusRow(id));
    } else if (e.key === "ArrowDown" || e.key === "j") step(1);
    else if (e.key === "ArrowUp" || e.key === "k") step(-1);
    else if (e.key === "Enter") open(id);
    else if (e.key === "Backspace" || e.key === "Delete") {
      const next = ids[i + 1] ?? ids[i - 1];
      remove(id);
      if (next) focusRow(next);
      else focusEl(document.querySelector<HTMLElement>(`[data-block="${b.id}"] [data-group-add]`));
    } else if (e.key === "m" || e.key === ".") setMenuFor(id);
    else if (e.key === "Escape") focusEl(document.querySelector<HTMLElement>(`[data-block="${b.id}"]`));
    else return;
    e.preventDefault();
    e.stopPropagation();
  };

  const others = groups.filter((g) => g.id !== b.id);
  return (
    <div
      className={"grp" + (over === "end" ? " over" : "")}
      onDragOver={(e) => onOver(e, "end")}
      onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(null); }}
      onDrop={(e) => onDrop(e, undefined)}
      data-group={b.id}
    >
      {items.length ? (
        <div ref={listRef} role="list" aria-label={`Cards in ${b.title}`} className="grpl" onDragOver={(e) => edgeScroll(e.currentTarget, e.clientY)} style={b.source?.height ? { maxHeight: b.source.height, overflowY: "auto" } : undefined}>
          {items.map((it, i) => (
            <GroupRow
              key={it.card.id}
              item={it}
              selected={board.selected?.id === it.card.id}
              over={over === i}
              menuOpen={menuFor === it.card.id}
              setMenuOpen={(o) => setMenuFor(o ? it.card.id : null)}
              others={others}
              onOpen={() => open(it.card.id)}
              onRemove={() => remove(it.card.id)}
              onMoveTo={(g, copy) => void move(it.card.id, b.id, g, undefined, copy)}
              onKeyDown={(e) => onRowKey(e, it.card.id, i)}
              onDragStart={(e) => {
                e.dataTransfer?.setData?.(GROUP_DRAG, JSON.stringify({ card: it.card.id, from: b.id }));
                e.dataTransfer?.setData?.("text/plain", it.card.id);
                if (e.dataTransfer) e.dataTransfer.effectAllowed = "copyMove";
              }}
              onDragOver={(e) => onOver(e, i)}
              onDrop={(e) => onDrop(e, i)}
            />
          ))}
        </div>
      ) : (
        <div className="hint grpempty">No cards yet. Add the ones you are working on, or drag one here from another group.</div>
      )}
      <div className="inline" style={{ marginTop: 8, position: "relative" }}>
        <button className="btn sm" type="button" data-group-add="" aria-expanded={picking} onClick={() => setPicking(!picking)}><Icon name="plus" size={12} />Add cards</button>
        {items.length ? <span className="hint" style={{ fontSize: 11 }}>Drag to move between groups, ⌥ to copy</span> : null}
        {picking ? <CardPicker board={board} exclude={ids} onPick={(id) => void mutate(() => api.card_group_add({ group: b.id, cards: [id] }))} onClose={() => setPicking(false)} /> : null}
      </div>
    </div>
  );
}

interface RowProps {
  item: GroupItem;
  selected: boolean;
  over: boolean;
  menuOpen: boolean;
  setMenuOpen: (open: boolean) => void;
  others: { id: string; title: string }[];
  onOpen: () => void;
  onRemove: () => void;
  onMoveTo: (group: string, copy: boolean) => void;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  onDragStart: (e: DragEvent) => void;
  onDragOver: (e: DragEvent) => void;
  onDrop: (e: DragEvent) => void;
}

function GroupRow({ item, selected, over, menuOpen, setMenuOpen, others, onOpen, onRemove, onMoveTo, onKeyDown, onDragStart, onDragOver, onDrop }: RowProps) {
  const { card: c, row } = item;
  const [dragging, setDragging] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);
  const last = row?.last ?? "";
  return (
    <div role="listitem" className={"grpr" + (over ? " over" : "") + (selected ? " sel" : "") + (dragging ? " dragging" : "") + (item.needsYou ? " ask" : "")}
      onDragOver={onDragOver} onDrop={onDrop}>
      <button
        type="button"
        className="grpc"
        draggable
        data-group-card={c.id}
        aria-label={`Open ${c.nick}${item.needsYou ? ", needs you" : ""}`}
        aria-current={selected ? "true" : undefined}
        title="↩ open the chat · ⌥↑↓ reorder · ⌫ remove · m move"
        onClick={onOpen}
        onKeyDown={onKeyDown}
        onDragStart={(e) => { onDragStart(e); setDragging(true); }}
        onDragEnd={() => setDragging(false)}
      >
        <span className="l1">
          <span className={`dot ${c.liveness}${c.liveness === "working" ? " pulse" : ""}`} title={c.liveness} />
          <span className="nick" title={c.nick}>{c.nick}</span>
          {c.key ? <span className="key" title={c.key}>{c.key}</span> : null}
          <span className="spacer" />
          {item.needsYou ? <span className="pill bad">needs you</span> : <span className={"pill" + (c.liveness === "working" ? " ok" : c.liveness === "waiting" ? " warn" : "")}>{LIVE_LABEL[c.liveness] ?? c.liveness}</span>}
        </span>
        <span className="l2">
          {item.column ? <span className="pill" title={`Column: ${item.column}`}>{item.column}</span> : null}
          {c.stage ? <span className="pill acc" title={`Stage: ${c.stage}`}>{c.stage}</span> : null}
          <span className="last" title={item.question || last}>{item.question ? `Q: ${short(item.question, 120)}` : last ? short(last, 140) : <i>No reports yet</i>}</span>
        </span>
      </button>
      <div style={{ position: "relative" }}>
        <button ref={menuBtn} type="button" className="ibtn" style={{ width: 24, height: 24 }} aria-label={`Actions for ${c.nick}`} aria-expanded={menuOpen} onClick={() => setMenuOpen(!menuOpen)}><Icon name="dots" size={13} /></button>
        {menuOpen ? (
          <GroupRowMenu others={others} nick={c.nick} opener={menuBtn} onClose={() => setMenuOpen(false)} onOpen={onOpen} onRemove={onRemove} onMoveTo={onMoveTo} />
        ) : null}
      </div>
    </div>
  );
}

function GroupRowMenu({ others, nick, opener, onClose, onOpen, onRemove, onMoveTo }: { others: { id: string; title: string }[]; nick: string; opener: { current: HTMLElement | null }; onClose: () => void; onOpen: () => void; onRemove: () => void; onMoveTo: (group: string, copy: boolean) => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>("button")?.focus();
  }, []);
  const act = (fn: () => void) => () => {
    fn();
    onClose();
  };
  return (
    <div className="menu" ref={ref} role="menu" aria-label={`Actions for ${nick}`} onKeyDown={(e) => { e.stopPropagation(); closeOnEscape(onClose, opener)(e); }}>
      <button className="btn sm" type="button" role="menuitem" onClick={act(onOpen)}>Open the chat</button>
      {others.map((g) => (
        <div key={g.id} className="inline" style={{ gap: 4 }}>
          <button className="btn sm" type="button" role="menuitem" style={{ flex: 1 }} onClick={act(() => onMoveTo(g.id, false))}>Move to {g.title}</button>
          <button className="btn sm" type="button" role="menuitem" aria-label={`Copy to ${g.title}`} title={`Copy to ${g.title}`} onClick={act(() => onMoveTo(g.id, true))}>Copy</button>
        </div>
      ))}
      <button className="btn sm danger" type="button" role="menuitem" onClick={act(onRemove)}>Remove from group</button>
    </div>
  );
}

/** A filterable list of the board's cards; picking one adds it. ↩ in the filter adds the first match. */
export function CardPicker({ board, exclude, onPick, onClose }: { board: BoardState; exclude: string[]; onPick: (id: string) => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  const cols = useMemo(() => new Map(board.columns.map((c) => [c.id, c.name])), [board.columns]);
  const needle = q.trim().toLowerCase();
  const matches = board.cards.filter((c) => !c.archived && !exclude.includes(c.id) && (!needle || `${c.nick} ${c.key} ${c.title}`.toLowerCase().includes(needle))).slice(0, 30);
  return (
    <div className="menu menu-l grppick" role="dialog" aria-label="Add cards to the group" onKeyDown={(e) => { e.stopPropagation(); closeOnEscape(onClose)(e); }}>
      <input ref={input} className="in" placeholder="Filter cards" aria-label="Filter cards" value={q} onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && matches[0]) {
            e.preventDefault();
            onPick(matches[0].id);
            setQ("");
          }
        }} />
      <div className="grppickl" role="list">
        {matches.length ? matches.map((c) => (
          <button key={c.id} type="button" role="listitem" className="btn sm" style={{ justifyContent: "flex-start" }} onClick={() => onPick(c.id)} aria-label={`Add ${c.nick}`}>
            <span className={`dot ${c.liveness}`} />
            <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textAlign: "left" }}>{c.nick}{c.key ? <span className="hint mono"> {c.key}</span> : null}</span>
            <span className="hint" style={{ fontSize: 11 }}>{cols.get(c.column_id) ?? ""}</span>
          </button>
        )) : <span className="hint">{board.cards.length ? "No other cards match." : "The board has no cards yet."}</span>}
      </div>
      <button className="btn sm" type="button" onClick={onClose}>Done</button>
    </div>
  );
}

/** "Watch in group…" for a card's menu: toggle the card in each group, or make a new group with it. */
export function WatchInGroup({ board, card }: { board: BoardState; card: Card }) {
  const wsId = board.ws?.id ?? "";
  const [groups, setGroups] = useState<api.CardGroup[] | null>(null);
  const [name, setName] = useState("");
  const load = () => {
    if (!wsId) return;
    api.card_group_list({ workspace_id: wsId }).then((g) => setGroups(g ?? [])).catch(() => setGroups([]));
  };
  useEffect(load, [wsId]); // eslint-disable-line react-hooks/exhaustive-deps
  const run = async <T,>(fn: () => Promise<T>) => {
    await board.actions.run(fn);
    load();
  };
  return (
    <fieldset className="grpwatch" aria-label="Watch in group">
      <legend>Watch in group…</legend>
      {groups === null ? <span className="hint">Loading…</span> : null}
      {groups?.map((g) => {
        const on = g.cards.some((r) => r.id === card.id);
        return (
          <label key={g.id} className="chk">
            <input type="checkbox" checked={on}
              onChange={() => void run(() => (on ? api.card_group_remove({ group: g.id, cards: [card.id] }) : api.card_group_add({ group: g.id, cards: [card.id] })))} />
            {g.title}
          </label>
        );
      })}
      <div className="inline" style={{ gap: 4 }}>
        <input className="in" style={{ flex: 1, minWidth: 0 }} placeholder="New group" aria-label="New group name" value={name} onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && name.trim()) {
              e.preventDefault();
              void run(() => api.card_group_create({ workspace_id: wsId, title: name.trim(), cards: [card.id] })).then(() => setName(""));
            }
          }} />
        <button className="btn sm" type="button" disabled={!name.trim()} onClick={() => void run(() => api.card_group_create({ workspace_id: wsId, title: name.trim(), cards: [card.id] })).then(() => setName(""))}>Add</button>
      </div>
    </fieldset>
  );
}
