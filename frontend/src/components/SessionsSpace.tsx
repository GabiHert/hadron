import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import * as api from "../api";
import type { FreeKind, FreeSession } from "../api";
import type { Board } from "../state/useBoard";
import { FREE_STARTED, freeTitle, inflight, placements, sessionsCtl, setActiveCwd, startFree, useFreeSessions } from "../state/freeSessions";
import type { Dir, Layout, Node } from "../sessionsLayout";
import * as L from "../sessionsLayout";
import { tildePath, slug } from "../lib";
import { withChord } from "../keys";
import { Terminal } from "./Terminal";
import type { TermHandle } from "./Terminal";
import { Icon } from "./ui";

interface Props {
  board: Board;
  /** the Board | Dashboard | Sessions switch */
  viewSeg: ReactNode;
  /** opens the "New session in a folder" dialog */
  onNewIn: () => void;
  /** asks before something that cannot be undone */
  confirm: (text: string, ok: string, run: () => void) => void;
}

/**
 * The Sessions space: free terminals in tabs and splits, keyboard-first. The sessions run in Go
 * whether or not this view is shown; only the panes of the shown tab hold a terminal, so a session
 * nobody looks at sends no output to the window.
 */
export function SessionsSpace({ board, viewSeg, onNewIn, confirm }: Props) {
  const { list, defaultCwd } = useFreeSessions();
  const [layout, setLayoutState] = useState<Layout>(L.EMPTY);
  const [loaded, setLoaded] = useState(false);
  const [titles, setTitles] = useState<Record<string, string>>({});
  const [renaming, setRenaming] = useState<string | null>(null);
  const [finding, setFinding] = useState<string | null>(null);
  const [carding, setCarding] = useState<string | null>(null);
  const [newMenu, setNewMenu] = useState(false);
  const [dragTab, setDragTab] = useState<string | null>(null);
  const handles = useRef(new Map<string, TermHandle>());
  const layoutRef = useRef(layout);
  layoutRef.current = layout;

  // The layout is kept in a setting, so tabs and splits come back after a restart.
  useEffect(() => {
    let live = true;
    Promise.resolve(api.settings_get({ key: L.SETTING_LAYOUT }))
      .then((s) => {
        if (live) setLayoutState(L.parseLayout(s?.[L.SETTING_LAYOUT]));
      })
      .catch(() => undefined)
      .finally(() => live && setLoaded(true));
    return () => void (live = false);
  }, []);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const setLayout = useCallback((next: Layout | ((l: Layout) => Layout)) => {
    setLayoutState((cur) => {
      const n = typeof next === "function" ? next(cur) : next;
      clearTimeout(saveTimer.current);
      saveTimer.current = setTimeout(() => void Promise.resolve(api.settings_update({ values: { [L.SETTING_LAYOUT]: n } })).catch(() => undefined), 300);
      return n;
    });
  }, []);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  // The sessions that exist decide the panes: a new one opens where it was asked for.
  const ids = useMemo(() => (list ?? []).map((s) => s.id), [list]);
  useEffect(() => {
    if (!loaded || list === null) return;
    setLayout((l) => {
      const shown = new Set(L.allPanes(l));
      const want = new Map(placements);
      // A session that shows up while this window's start call is still out is that start's.
      for (const id of ids) if (!shown.has(id) && !want.has(id) && inflight.current) want.set(id, inflight.current);
      const next = L.reconcile(l, ids, want);
      for (const id of ids) placements.delete(id);
      return JSON.stringify(next) === JSON.stringify(l) ? l : next;
    });
  }, [ids, loaded, list, setLayout]);

  const byId = useMemo(() => new Map((list ?? []).map((s) => [s.id, s])), [list]);
  const tab = L.activeTab(layout);
  const activeId = L.activePane(layout);
  const active = byId.get(activeId);
  useEffect(() => setActiveCwd(active?.cwd ?? ""), [active?.cwd]);
  // The focused pane's terminal takes the keyboard when the tab or the pane changes.
  useEffect(() => {
    const h = handles.current.get(activeId);
    if (h && !renaming && !finding && !carding) h.focus();
  }, [activeId, renaming, finding, carding]);

  const title = (s: FreeSession) => freeTitle(s, titles[s.id]);
  const toast = board.actions.toast;
  const run = async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
    try {
      return await fn();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e));
      return undefined;
    }
  };
  const start = (kind: FreeKind, split?: Dir) => {
    setNewMenu(false);
    void run(() => startFree({ kind, ...(split && activeId ? { split: { pane: activeId, dir: split } } : {}) }));
  };
  const closePane = (id: string) => {
    const s = byId.get(id);
    const go = () => {
      setLayout((l) => L.close(l, id));
      void run(() => api.free_session_stop({ session: id, close: true }));
    };
    // A shell at its prompt closes at once; a program at work, or claude, asks first.
    const busy = s && s.running && (s.kind === "claude" || (s.program && !/^-?(zsh|bash|sh|fish|tcsh|ksh|dash)$/.test(s.program)));
    if (busy) confirm(`Close “${title(s)}”? It is running ${s.program || s.kind}, which stops.`, "Close", go);
    else go();
  };

  useEffect(() => {
    sessionsCtl.current = {
      active: () => byId.get(L.activePane(layoutRef.current)),
      split: (dir, kind = "shell") => start(kind, dir),
      close: () => {
        const id = L.activePane(layoutRef.current);
        if (id) closePane(id);
      },
      stepTab: (d) => setLayout((l) => L.stepTab(l, d)),
      tab: (n) => setLayout((l) => L.selectTab(l, n - 1)),
      focus: (dir) => {
        const id = L.neighbor(layoutRef.current, dir);
        if (id) setLayout((l) => L.focus(l, id));
      },
      moveTab: (d) => {
        const t = L.activeTab(layoutRef.current);
        if (t) setLayout((l) => L.moveTab(l, t.id, d));
      },
      rename: () => setRenaming(L.activePane(layoutRef.current) || null),
      clear: () => handles.current.get(L.activePane(layoutRef.current))?.clear(),
      find: () => setFinding(L.activePane(layoutRef.current) || null),
      restart: () => {
        const id = L.activePane(layoutRef.current);
        if (id) void run(() => api.free_session_restart({ session: id }));
      },
      toCard: () => setCarding(L.activePane(layoutRef.current) || null),
    };
    return () => {
      sessionsCtl.current = null;
    };
  });

  // A session started elsewhere (the menu, ⌘T from the board) lands here with the focus.
  useEffect(() => {
    const on = (e: Event) => {
      const row = (e as CustomEvent<FreeSession>).detail;
      if (row?.id) setTimeout(() => setLayout((l) => (L.tabOf(l, row.id) ? L.focus(l, row.id) : l)), 120);
    };
    window.addEventListener(FREE_STARTED, on);
    return () => window.removeEventListener(FREE_STARTED, on);
  }, [setLayout]);

  const ws = board.ws;
  const tabTitle = (t: L.Tab) => {
    const s = byId.get(t.active) ?? byId.get(L.panes(t.root)[0]);
    const n = L.panes(t.root).length;
    return (s ? title(s) : "…") + (n > 1 ? ` +${n - 1}` : "");
  };

  const renderNode = (n: Node): ReactNode => {
    if ("pane" in n) {
      const s = byId.get(n.pane);
      return s ? (
        <Pane
          key={n.pane}
          s={s}
          title={title(s)}
          focused={n.pane === activeId}
          renaming={renaming === n.pane}
          finding={finding === n.pane}
          carding={carding === n.pane}
          board={board}
          onFocus={() => n.pane !== L.activePane(layoutRef.current) && setLayout((l) => L.focus(l, n.pane))}
          onTitle={(t) => setTitles((x) => (x[n.pane] === t ? x : { ...x, [n.pane]: t }))}
          onHandle={(h) => (h ? handles.current.set(n.pane, h) : handles.current.delete(n.pane))}
          handle={() => handles.current.get(n.pane)}
          onRename={() => setRenaming(n.pane)}
          endRename={() => setRenaming(null)}
          endFind={() => setFinding(null)}
          endCard={() => setCarding(null)}
          onSplit={(dir) => {
            setLayout((l) => L.focus(l, n.pane));
            void run(() => startFree({ kind: "shell", cwd: s.cwd || undefined, split: { pane: n.pane, dir } }));
          }}
          onClose={() => closePane(n.pane)}
          onRestart={() => void run(() => api.free_session_restart({ session: n.pane }))}
          onCard={() => setCarding(n.pane)}
          run={run}
        />
      ) : null;
    }
    return (
      <Split key={n.id} node={n} onRatio={(r) => setLayout((l) => L.setRatio(l, n.id, r))}>
        {renderNode(n.a)}
        {renderNode(n.b)}
      </Split>
    );
  };

  return (
    <main className="board sspace" data-zone="sessions" tabIndex={-1} aria-label="Sessions">
      <div className="bhead">
        <div className="r1">
          <h1>Sessions</h1>
          {viewSeg}
          <span className="cap" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>
            terminals of your own, in every workspace{defaultCwd ? <> · new ones open in <span className="mono">{tildePath(defaultCwd)}</span></> : null}
          </span>
          <div className="spacer" />
          <div className="newwrap">
            <button className="btn pri" onClick={() => start("shell")} title={withChord("New terminal", "sessions.new")}>
              <Icon name="term" size={13} />New terminal
            </button>
            <button className="btn" aria-label="More ways to start a session" aria-haspopup="menu" aria-expanded={newMenu} onClick={() => setNewMenu((o) => !o)}>
              <Icon name="down" size={12} />
            </button>
            {newMenu ? (
              <div className="menu" role="menu" onKeyDown={(e) => e.key === "Escape" && setNewMenu(false)}>
                <button role="menuitem" autoFocus onClick={() => start("shell")}>New terminal<span className="kbd">{withChord("", "sessions.new").replace(/[()]/g, "").trim()}</span></button>
                <button role="menuitem" onClick={() => start("claude")}>New claude session<span className="kbd">{withChord("", "sessions.newClaude").replace(/[()]/g, "").trim()}</span></button>
                <button role="menuitem" onClick={() => { setNewMenu(false); onNewIn(); }}>In a folder, with a name…<span className="kbd">{withChord("", "sessions.newIn").replace(/[()]/g, "").trim()}</span></button>
              </div>
            ) : null}
          </div>
        </div>
        <div className="stabs" role="tablist" aria-label="Session tabs">
          {layout.tabs.map((t, i) => {
            const s = byId.get(t.active);
            const on = t.id === tab?.id;
            return (
              <div
                key={t.id}
                role="tab"
                aria-selected={on}
                tabIndex={on ? 0 : -1}
                className={"stab" + (on ? " on" : "") + (dragTab === t.id ? " drag" : "")}
                title={s ? `${title(s)} · ${tildePath(s.cwd)}${i < 9 ? ` (⌥⌘${i + 1})` : ""}` : ""}
                draggable
                onDragStart={(e) => {
                  setDragTab(t.id);
                  e.dataTransfer.effectAllowed = "move";
                }}
                onDragOver={(e) => dragTab && e.preventDefault()}
                onDrop={(e) => {
                  e.preventDefault();
                  if (dragTab) setLayout((l) => L.placeTab(l, dragTab, i));
                  setDragTab(null);
                }}
                onDragEnd={() => setDragTab(null)}
                onClick={() => setLayout((l) => ({ ...l, active: t.id }))}
                onDoubleClick={() => {
                  setLayout((l) => ({ ...l, active: t.id }));
                  setRenaming(t.active);
                }}
              >
                <span className={"dot " + (s?.running ? s.liveness : "ended")} aria-hidden />
                <span className="stitle">{tabTitle(t)}</span>
                {s?.workspace_id && s.workspace_id !== ws?.id ? <span className="stag">{board.visible.find((w) => w.id === s.workspace_id)?.name ?? ""}</span> : null}
                <button
                  className="sx"
                  aria-label={`Close ${tabTitle(t)}`}
                  title="Close the tab"
                  onClick={(e) => {
                    e.stopPropagation();
                    for (const p of L.panes(t.root)) closePane(p);
                  }}
                >
                  <Icon name="close" size={11} />
                </button>
              </div>
            );
          })}
          <button className="stab add" aria-label="New terminal" title={withChord("New terminal", "sessions.new")} onClick={() => start("shell")}>
            <Icon name="plus" size={12} />
          </button>
        </div>
      </div>
      <div className="spanes">
        {tab ? (
          renderNode(tab.root)
        ) : list === null ? (
          <div className="empty" role="status" style={{ margin: "auto" }}>Loading your sessions…</div>
        ) : (
          <div className="empty" style={{ margin: "auto", display: "flex", flexDirection: "column", gap: 10, alignItems: "center", textAlign: "center" }}>
            <span>No sessions yet. A session here is a terminal of your own: your login shell, or claude, in any folder. It keeps running in every workspace and comes back after Hadron restarts.</span>
            <div style={{ display: "flex", gap: 8 }}>
              <button className="btn pri" onClick={() => start("shell")}><Icon name="term" size={13} />{withChord("New terminal", "sessions.new")}</button>
              <button className="btn" onClick={() => start("claude")}>{withChord("New claude session", "sessions.newClaude")}</button>
            </div>
          </div>
        )}
      </div>
    </main>
  );
}

function Split({ node, onRatio, children }: { node: Extract<Node, { split: Dir }>; onRatio: (r: number) => void; children: ReactNode[] }) {
  const box = useRef<HTMLDivElement>(null);
  const row = node.split === "row";
  const drag = (e: React.PointerEvent) => {
    const el = box.current;
    if (!el) return;
    e.preventDefault();
    const r = el.getBoundingClientRect();
    const move = (ev: PointerEvent) => onRatio(row ? (ev.clientX - r.left) / r.width : (ev.clientY - r.top) / r.height);
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const pct = `${node.ratio * 100}%`;
  return (
    <div ref={box} className={"ssplit " + (row ? "row" : "col")}>
      <div className="sside" style={{ flexBasis: pct }}>{children[0]}</div>
      <div
        className="sgut"
        role="separator"
        aria-orientation={row ? "vertical" : "horizontal"}
        aria-valuenow={Math.round(node.ratio * 100)}
        tabIndex={0}
        onPointerDown={drag}
        onKeyDown={(e) => {
          const d = (row ? { ArrowLeft: -0.05, ArrowRight: 0.05 } : { ArrowUp: -0.05, ArrowDown: 0.05 })[e.key as "ArrowLeft"];
          if (d) {
            e.preventDefault();
            onRatio(node.ratio + d);
          }
        }}
      />
      <div className="sside" style={{ flexBasis: `calc(100% - ${pct})` }}>{children[1]}</div>
    </div>
  );
}

interface PaneProps {
  s: FreeSession;
  title: string;
  focused: boolean;
  renaming: boolean;
  finding: boolean;
  carding: boolean;
  board: Board;
  onFocus: () => void;
  onTitle: (t: string) => void;
  onHandle: (h: TermHandle | null) => void;
  handle: () => TermHandle | undefined;
  onRename: () => void;
  endRename: () => void;
  endFind: () => void;
  endCard: () => void;
  onSplit: (dir: Dir) => void;
  onClose: () => void;
  onRestart: () => void;
  onCard: () => void;
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>;
}

function Pane(p: PaneProps) {
  const { s } = p;
  const [name, setName] = useState(s.name);
  const [query, setQuery] = useState("");
  const [miss, setMiss] = useState(false);
  useEffect(() => setName(s.name), [s.name, p.renaming]);
  const saveName = async () => {
    p.endRename();
    if (name.trim() !== s.name) await p.run(() => api.free_session_update({ session: s.id, name: name.trim() }));
  };
  const find = (back = false) => {
    const h = p.handle();
    setMiss(!!query && !!h && !h.find(query, back));
  };
  return (
    <section className={"spane" + (p.focused ? " on" : "")} aria-label={`Session ${p.title}`} onMouseDown={p.onFocus}>
      <div className="sph">
        <span className={"dot " + (s.running ? s.liveness : "ended")} aria-hidden />
        {p.renaming ? (
          <input
            className="in sren"
            aria-label="Session name"
            autoFocus
            value={name}
            placeholder={freeTitle({ ...s, name: "" })}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => void saveName()}
            onKeyDown={(e) => {
              if (e.key === "Enter") void saveName();
              if (e.key === "Escape") {
                setName(s.name);
                p.endRename();
              }
              e.stopPropagation();
            }}
          />
        ) : (
          <strong className="sname" title="Double-click to rename" onDoubleClick={p.onRename}>{p.title}</strong>
        )}
        <span className="mono sdir">{tildePath(s.cwd)}</span>
        {s.kind === "claude" ? <span className="chip">claude</span> : null}
        <div className="spacer" />
        <button className="tbtn" title={withChord("Split right", "sessions.splitRight")} aria-label="Split right" onClick={() => p.onSplit("row")}><Icon name="board" size={12} /></button>
        <button className="tbtn" title={withChord("Split down", "sessions.splitDown")} aria-label="Split down" onClick={() => p.onSplit("col")}><Icon name="dash" size={12} /></button>
        <button className="tbtn" title="Make a card of it, without stopping it" onClick={p.onCard}>Make a card</button>
        <button className="tbtn" title={withChord("Close", "sessions.close")} aria-label="Close the pane" onClick={p.onClose}><Icon name="close" size={12} /></button>
      </div>
      {p.finding ? (
        <div className="sfind" role="search">
          <input
            className="in"
            autoFocus
            aria-label="Find in the scrollback"
            placeholder="Find"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              const h = p.handle();
              setMiss(!!e.target.value && !!h && !h.find(e.target.value));
            }}
            onKeyDown={(e) => {
              e.stopPropagation();
              if (e.key === "Enter") find(e.shiftKey);
              if (e.key === "Escape") {
                p.handle()?.endFind();
                p.endFind();
              }
            }}
          />
          <span className="hint" role="status">{miss ? "No match" : "↩ next · ⇧↩ previous · Esc"}</span>
          <button className="tbtn" aria-label="Close find" onClick={() => { p.handle()?.endFind(); p.endFind(); }}><Icon name="close" size={11} /></button>
        </div>
      ) : null}
      {p.carding ? <MakeCard s={s} board={p.board} onClose={p.endCard} run={p.run} /> : null}
      <div className="sterm">
        <Terminal
          sessionId={s.id}
          label={p.title}
          toolbar={s.kind === "claude"}
          free={{ shell: s.kind === "shell", onTitle: p.onTitle, onHandle: p.onHandle, onFocus: p.onFocus }}
        />
        {!s.running ? (
          <div className="sended" role="status">
            <span>{s.error ? `Could not start: ${s.error}` : `Ended${s.exit_code ? ` (exit ${s.exit_code})` : ""}.`}</span>
            <button className="btn pri" onClick={p.onRestart}>{s.kind === "claude" ? "Resume" : "Start again"}</button>
            <button className="btn" onClick={p.onClose}>Close</button>
          </div>
        ) : null}
      </div>
    </section>
  );
}

/** Turns a free session into a card on a board, keeping its process. */
function MakeCard({ s, board, onClose, run }: { s: FreeSession; board: Board; onClose: () => void; run: PaneProps["run"] }) {
  const [wsId, setWsId] = useState(s.workspace_id || board.ws?.id || "");
  const [nick, setNick] = useState(slug(s.name || freeTitle({ ...s, name: "" }).replace(/·/g, " ")).slice(0, 40));
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    const out = await run(() => api.free_session_to_card({ session: s.id, workspace_id: wsId, nick }));
    setBusy(false);
    if (!out) return;
    onClose();
    board.actions.toast(`It is now card ${out.card.nick}${out.warning ? `. ${out.warning}` : "."}`);
  };
  return (
    <form
      className="scard"
      aria-label="Make a card"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Escape") onClose();
      }}
    >
      <span className="hint">The session keeps running and moves to a card on</span>
      <select className="in" aria-label="Board" value={wsId} onChange={(e) => setWsId(e.target.value)}>
        {board.visible.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
      </select>
      <input className="in mono" aria-label="Card nickname" autoFocus value={nick} onChange={(e) => setNick(slug(e.target.value))} placeholder="nickname" />
      <button className="btn pri" type="submit" disabled={!nick || !wsId || busy}>Make the card</button>
      <button className="btn" type="button" onClick={onClose}>Cancel</button>
    </form>
  );
}
