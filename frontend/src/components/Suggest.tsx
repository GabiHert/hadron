import { useEffect, useMemo, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode } from "react";
import * as api from "../api";
import type { Card, Column, ComposerTarget, FileHit, SlashCommand } from "../api";
import { applyPick, findTrigger, fuzzy, pathMention } from "../complete";
import type { Trigger } from "../complete";

/** The most rows the menu shows before it scrolls. */
export const SUGGEST_ROWS = 8;
/** How many cards the @ menu lists before the files. */
const MAX_CARDS = 6;
/** How many files one search asks for. */
const FILE_LIMIT = 40;
/** How long the @ menu waits after a keystroke before it searches the folder. */
export const FILE_DEBOUNCE_MS = 90;

export interface SuggestItem {
  id: string;
  group: "Commands" | "Cards" | "Files";
  /** the text shown, with the matched characters in idx */
  label: string;
  idx: number[];
  mono: boolean;
  detail: string;
  badge: string;
  insert: string;
}

/** The text with its matched characters marked. */
export function Hl({ text, idx }: { text: string; idx: number[] }) {
  if (!idx.length) return <>{text}</>;
  const on = new Set(idx);
  const out: ReactNode[] = [];
  let run = "";
  let marked = false;
  const flush = (k: number) => {
    if (!run) return;
    out.push(marked ? <mark key={k}>{run}</mark> : <span key={k}>{run}</span>);
    run = "";
  };
  for (let i = 0; i < text.length; i++) {
    const m = on.has(i);
    if (m !== marked) {
      flush(i);
      marked = m;
    }
    run += text[i];
  }
  flush(text.length);
  return <>{out}</>;
}

interface Opts {
  id: string;
  value: string;
  caret: number;
  /** false while the textarea is blurred or an input method composes: the menu stays shut */
  active: boolean;
  target?: ComposerTarget;
  /** "/" commands are offered only when slash commands go to the terminal */
  commands: boolean;
  cards?: Card[];
  columns?: Column[];
  /** offer @planner (not in the planner's own chat) */
  planner: boolean;
  /** puts the picked text into the field and the caret after it */
  onPick: (value: string, caret: number) => void;
}

/**
 * The state of the composer's / and @ menu. It reads the session's commands once per target and
 * searches its folder as you type after @. The textarea keeps the focus all along: keys reach the
 * menu through onKeyDown, and a click on a row is a mousedown that never blurs the field.
 */
export function useSuggest({ id, value, caret, active, target, commands, cards = [], columns = [], planner, onPick }: Opts) {
  const trigger = active ? findTrigger(value, caret) : null;
  const usable = trigger && (trigger.kind === "@" ? !!target : commands && !!target) ? trigger : null;
  const [dismissed, setDismissed] = useState("");
  const [sel, setSel] = useState(0);
  const targetKey = target ? `${target.workspace_id ?? ""}|${target.card ?? ""}|${target.planner ? "p" : ""}` : "";
  const [cmds, setCmds] = useState<{ key: string; list: SlashCommand[] }>({ key: "", list: [] });
  const [found, setFound] = useState<{ key: string; q: string; list: FileHit[] }>({ key: "", q: "", list: [] });
  const seq = useRef(0);
  const tKey = usable ? `${usable.kind}:${usable.start}` : "";
  const open = !!usable && tKey !== dismissed;

  // Esc shuts the menu for this token only; a new / or @ opens it again.
  useEffect(() => {
    if (dismissed && dismissed !== tKey) setDismissed("");
  }, [tKey, dismissed]);

  const wantCmds = open && usable?.kind === "/" && cmds.key !== targetKey;
  useEffect(() => {
    if (!wantCmds || !target) return;
    let live = true;
    api.session_commands_list(target).then(
      (list) => live && setCmds({ key: targetKey, list: list ?? [] }),
      () => live && setCmds({ key: targetKey, list: [] }),
    );
    return () => {
      live = false;
    };
  }, [wantCmds, targetKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const fileQ = open && usable?.kind === "@" ? usable.query : null;
  useEffect(() => {
    if (fileQ === null || !target) return;
    const mine = ++seq.current;
    const t = setTimeout(() => {
      api.files_search({ ...target, q: fileQ.replace(/^"/, ""), limit: FILE_LIMIT }).then(
        (list) => mine === seq.current && setFound({ key: targetKey, q: fileQ, list: list ?? [] }),
        () => mine === seq.current && setFound({ key: targetKey, q: fileQ, list: [] }),
      );
    }, FILE_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [fileQ, targetKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const items = useMemo<SuggestItem[]>(() => {
    if (!open || !usable) return [];
    const q = usable.query;
    if (usable.kind === "/") {
      if (cmds.key !== targetKey) return [];
      return cmds.list
        .map((c) => ({ c, m: fuzzy(q, c.name) }))
        .filter((x): x is { c: SlashCommand; m: NonNullable<ReturnType<typeof fuzzy>> } => !!x.m)
        .sort((a, b) => b.m.score - a.m.score)
        .slice(0, 60)
        .map(({ c, m }) => ({ id: `cmd:${c.source}:${c.name}`, group: "Commands" as const, label: "/" + c.name, idx: m.idx.map((i) => i + 1), mono: true, detail: c.description, badge: c.kind === "skill" ? `${c.source} skill` : c.source, insert: "/" + c.name }));
    }
    const colName = new Map(columns.map((c) => [c.id, c.name]));
    const people: (SuggestItem & { score: number })[] = [];
    if (planner) {
      const m = fuzzy(q, "planner");
      if (m) people.push({ id: "planner", group: "Cards", label: "@planner", idx: m.idx.map((i) => i + 1), mono: true, detail: "The workspace's planner", badge: "planner", insert: "@planner", score: m.score + 50 });
    }
    for (const c of cards) {
      if (c.archived) continue;
      const byNick = fuzzy(q, c.nick);
      const other = byNick ? null : fuzzy(q, c.key) ?? fuzzy(q, c.title ?? "");
      const m = byNick ?? other;
      if (!m) continue;
      const detail = [c.key, c.title].filter(Boolean).join(" · ");
      people.push({ id: "card:" + c.id, group: "Cards", label: "@" + c.nick, idx: byNick ? byNick.idx.map((i) => i + 1) : [], mono: true, detail, badge: colName.get(c.column_id) ?? "", insert: "@" + c.nick, score: m.score - (byNick ? 0 : 200) });
    }
    people.sort((a, b) => b.score - a.score);
    const fq = q.replace(/^"/, "");
    const filesNow = found.key === targetKey ? found.list : [];
    const fileItems: SuggestItem[] = [];
    for (const f of filesNow) {
      const m = fuzzy(fq, f.path);
      if (!m) continue;
      const label = f.is_dir ? f.path + "/" : f.path;
      fileItems.push({ id: "file:" + f.path, group: "Files", label, idx: m.idx, mono: true, detail: "", badge: f.is_dir ? "folder" : "", insert: pathMention(f.path, f.is_dir) });
    }
    return [...people.slice(0, MAX_CARDS).map(({ score: _s, ...it }) => it), ...fileItems];
  }, [open, usable?.kind, usable?.query, cmds, found, cards, columns, planner, targetKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const shown = open && items.length > 0;
  const qKey = `${tKey}|${usable?.query ?? ""}`;
  useEffect(() => setSel(0), [qKey]);
  const cur = Math.min(sel, Math.max(items.length - 1, 0));

  const pick = (i: number) => {
    const it = items[i];
    if (!it || !usable) return;
    const { value: nv, caret: nc } = applyPick(value, usable as Trigger, it.insert);
    onPick(nv, nc);
  };

  /** Handles a key for the menu; true when it took the key. */
  const onKeyDown = (e: ReactKeyboardEvent<HTMLTextAreaElement>): boolean => {
    if (!shown) return false;
    const n = items.length;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setSel((cur + 1) % n);
        return true;
      case "ArrowUp":
        e.preventDefault();
        setSel((cur - 1 + n) % n);
        return true;
      case "Enter":
      case "Tab":
        if (e.shiftKey) return false;
        // Enter on a command typed out in full sends it, as in Claude Code; Tab still completes.
        if (e.key === "Enter" && usable?.kind === "/" && items[cur]?.insert === value.slice(usable.start, usable.end)) return false;
        e.preventDefault();
        pick(cur);
        return true;
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        setDismissed(tKey);
        return true;
    }
    return false;
  };

  const listId = `${id}-suggest`;
  const optId = (i: number) => `${listId}-${i}`;
  // A textarea stays a textbox (ARIA's combobox is single-line); these tie it to the menu.
  const ariaProps = {
    "aria-autocomplete": "list" as const,
    "aria-controls": shown ? listId : undefined,
    "aria-activedescendant": shown ? optId(cur) : undefined,
  };

  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!shown) return;
    const el = listRef.current?.querySelector<HTMLElement>('[aria-selected="true"]');
    el?.scrollIntoView?.({ block: "nearest" });
  }, [cur, shown]);

  const menu = shown ? (
    <div
      ref={listRef}
      className="suggest"
      id={listId}
      role="listbox"
      aria-label={usable?.kind === "/" ? "Slash commands" : "Mentions"}
      // A mousedown on the menu would blur the textarea and shut the menu before the click lands.
      onMouseDown={(e) => e.preventDefault()}
    >
      {items.map((it, i) => (
        <SuggestRow key={it.id} it={it} id={optId(i)} on={i === cur} head={i === 0 || items[i - 1].group !== it.group} onHover={() => setSel(i)} onPick={() => pick(i)} />
      ))}
    </div>
  ) : null;

  return { open: shown, items, sel: cur, menu, onKeyDown, ariaProps, kind: usable?.kind ?? null };
}

function SuggestRow({ it, id, on, head, onHover, onPick }: { it: SuggestItem; id: string; on: boolean; head: boolean; onHover: () => void; onPick: () => void }) {
  return (
    <>
      {head && it.group !== "Commands" ? <div className="sg-h" role="presentation">{it.group}</div> : null}
      <div id={id} role="option" aria-selected={on} className={"sg-row" + (on ? " on" : "")} onMouseMove={on ? undefined : onHover} onClick={onPick}>
        <span className={"sg-name" + (it.mono ? " mono" : "")}><Hl text={it.label} idx={it.idx} /></span>
        {it.detail ? <span className="sg-desc">{it.detail}</span> : <span className="sg-desc" />}
        {it.badge ? <span className="sg-src">{it.badge}</span> : null}
      </div>
    </>
  );
}
