import { useEffect, useRef, useState } from "react";
import { DEFAULTS, cheatSheet, fuzzyScore, keysOf, formatChord, speakChord } from "../keys";
import type { CommandDef, Overrides } from "../keys";
import type { Board } from "../state/useBoard";
import { ModalHead, SETTINGS_TABS, Scrim } from "./Modals";

// ---- the palette ----

export interface PaletteItem {
  /** stable key, kept in the recent list */
  key: string;
  label: string;
  /** the chord, a card key or the kind of item */
  hint: string;
  group: string;
  run: () => void;
}

/**
 * Registry commands the palette leaves out, and why: they only make sense as keys in a place
 * (moving through a list), or the palette has a better row for them (Switch workspace: …).
 */
export const PALETTE_SKIP: Record<string, string> = {
  "palette.toggle": "the palette itself",
  escape: "a key, not a command",
  "board.up": "moving through the board",
  "board.down": "moving through the board",
  "board.left": "moving through the board",
  "board.right": "moving through the board",
  "board.goto": "Go to card: … rows",
  "panel.nextMessage": "moving through a chat",
  "panel.prevMessage": "moving through a chat",
  "dash.next": "moving through the dashboard",
  "dash.prev": "moving through the dashboard",
  "terminal.leave": "only from inside a terminal",
  ...Object.fromEntries([1, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => [`workspace.${n}`, "Switch workspace: … rows"])),
  ...Object.fromEntries([1, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => [`question.pick.${n}`, "click or press the option's number"])),
};

const RECENT = "hadron.palette.recent";
function readRecent(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(RECENT) ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}
function pushRecent(key: string) {
  try {
    localStorage.setItem(RECENT, JSON.stringify([key, ...readRecent().filter((k) => k !== key)].slice(0, 12)));
  } catch {
    // private window or storage blocked: the palette just forgets
  }
}

/** The palette's rows: every command with its chord, every card, workspace and Settings section. */
export function paletteItems(board: Board, overrides: Overrides, run: (id: string) => void, openSettings: (tab: string) => void): PaletteItem[] {
  const c = board.selected;
  const cardCmd = (d: CommandDef) => d.group === "Card" || d.id === "board.open" || d.id === "board.peek";
  const cmds = DEFAULTS.filter((d) => !(d.id in PALETTE_SKIP)).filter((d) => !cardCmd(d) || c);
  return [
    ...cmds.map((d) => ({
      key: `cmd:${d.id}`,
      label: cardCmd(d) && c ? `${d.title}: ${c.nick}` : d.title,
      hint: keysOf(d, overrides).map(formatChord)[0] ?? "",
      group: d.group,
      run: () => run(d.id),
    })),
    ...board.visible.map((w, i) => ({ key: `ws:${w.id}`, label: `Switch workspace: ${w.name}`, hint: i < 9 ? formatChord(keysOf(DEFAULTS.find((d) => d.id === `workspace.${i + 1}`)!, overrides)[0] ?? "") : "workspace", group: "Workspaces", run: () => void board.actions.switchWorkspace(w.id) })),
    ...board.cards.map((x) => ({ key: `card:${x.id}`, label: `Go to card: ${x.nick}`, hint: x.key || "card", group: "Cards", run: () => run(`goto:${x.id}`) })),
    ...SETTINGS_TABS.map((t) => ({ key: `settings:${t.id}`, label: `Open Settings: ${t.label}`, hint: "", group: "Settings", run: () => openSettings(t.id) })),
  ];
}

/** Recent first with no query; with one, the best fuzzy matches, recent ones breaking ties. */
export function rankItems(items: PaletteItem[], q: string, recent: string[]): PaletteItem[] {
  const rank = (k: string) => {
    const i = recent.indexOf(k);
    return i < 0 ? 0 : 50 - i;
  };
  if (!q.trim()) {
    const rec = recent.map((k) => items.find((x) => x.key === k)).filter((x): x is PaletteItem => !!x);
    return [...rec, ...items.filter((x) => !recent.includes(x.key))];
  }
  return items
    .map((x) => ({ x, s: fuzzyScore(x.label, q) }))
    .filter((r) => r.s >= 0)
    .sort((a, b) => b.s + rank(b.x.key) - (a.s + rank(a.x.key)))
    .map((r) => r.x);
}

export function Palette({ items, onClose }: { items: PaletteItem[]; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [i, setI] = useState(0);
  const ref = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const [recent] = useState(readRecent);
  useEffect(() => ref.current?.focus(), []);
  const shown = rankItems(items, q, recent).slice(0, 60);
  useEffect(() => {
    list.current?.querySelector<HTMLElement>(`[data-i="${i}"]`)?.scrollIntoView?.({ block: "nearest" });
  }, [i]);
  const go = (x?: PaletteItem) => {
    if (!x) return;
    pushRecent(x.key);
    onClose();
    // After the dialog has given the focus back, so a card command acts on the card it was on.
    setTimeout(() => x.run(), 0);
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal pal" role="dialog" aria-modal="true" aria-label="Command palette">
        <input
          ref={ref}
          id="pal-q"
          placeholder="Jump to a card, workspace or command"
          value={q}
          role="combobox"
          aria-expanded="true"
          aria-controls="pal-list"
          aria-activedescendant={shown[i] ? `pal-${i}` : undefined}
          onChange={(e) => { setQ(e.target.value); setI(0); }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) { e.preventDefault(); setI(Math.min(i + 1, shown.length - 1)); }
            if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) { e.preventDefault(); setI(Math.max(i - 1, 0)); }
            if (e.key === "Enter") { e.preventDefault(); go(shown[i]); }
          }}
        />
        <ul id="pal-list" role="listbox" ref={list} aria-label="Commands">
          {shown.map((x, j) => (
            <li key={x.key} role="presentation">
              <button id={`pal-${j}`} data-i={j} role="option" aria-selected={j === i} tabIndex={-1} className={j === i ? "on" : ""} onClick={() => go(x)} onMouseMove={() => j !== i && setI(j)}>
                <span style={{ flex: 1 }}>{x.label}</span>
                <span className="hint">{x.hint}</span>
              </button>
            </li>
          ))}
          {!shown.length ? <li className="hint" style={{ padding: 10 }}>Nothing matches.</li> : null}
        </ul>
      </div>
    </Scrim>
  );
}

// ---- the cheat sheet ----

export function CheatSheet({ overrides, onClose, onSettings }: { overrides: Overrides; onClose: () => void; onSettings: () => void }) {
  const groups = cheatSheet(overrides);
  return (
    <Scrim onClose={onClose}>
      <div className="modal wide" role="dialog" aria-modal="true" aria-labelledby="ks-t">
        <ModalHead id="ks-t" title="Keyboard shortcuts" onClose={onClose} />
        <div className="mb cheat" tabIndex={0} aria-label="Shortcuts by area">
          {groups.map((g) => (
            <section key={g.group} aria-label={g.group}>
              <h5 className="cap">{g.group}</h5>
              <dl>
                {g.rows.map((r) => (
                  <div key={r.ids.join(",")} className="crow">
                    <dt>{r.title}</dt>
                    <dd>{r.keys.map((k) => <kbd key={k} className="kbd" aria-label={speakChord(k)}>{k}</kbd>)}</dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}
        </div>
        <div className="mf">
          <span className="hint" style={{ flex: 1 }}>Single keys work when no text field has the focus. In a terminal only ⌘ chords are Hadron's; ⌘Esc leaves it.</span>
          <button className="btn" onClick={onSettings}>Change shortcuts…</button>
          <button className="btn pri" onClick={onClose}>Done</button>
        </div>
      </div>
    </Scrim>
  );
}

// ---- confirm ----

export interface ConfirmReq {
  text: string;
  ok: string;
  run: () => void;
}

/** Asks before a destructive command. ↩ confirms, Esc cancels; the confirm button has the focus. */
export function ConfirmDialog({ req, onClose }: { req: ConfirmReq; onClose: () => void }) {
  const ref = useRef<HTMLButtonElement>(null);
  useEffect(() => ref.current?.focus(), []);
  return (
    <Scrim onClose={onClose}>
      <div className="modal confirm" role="alertdialog" aria-modal="true" aria-labelledby="cf-t" aria-describedby="cf-d">
        <ModalHead id="cf-t" title="Are you sure?" onClose={onClose} />
        <div className="mb"><p id="cf-d" style={{ margin: 0 }}>{req.text}</p></div>
        <div className="mf">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button
            ref={ref}
            className="btn pri danger"
            onClick={() => {
              onClose();
              setTimeout(req.run, 0);
            }}
          >
            {req.ok}
          </button>
        </div>
      </div>
    </Scrim>
  );
}
