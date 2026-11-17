// The Sessions space's shared state: the free sessions (from Go, refetched on their change events),
// where a session about to start should go (a split of the focused pane, else a new tab), and the
// controller the mounted space registers so the keyboard commands can reach it.
import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { FreeKind, FreeSession } from "../api";
import type { Dir, Placement } from "../sessionsLayout";

/** The change entity Go sends when free sessions change. */
export const ENTITY_FREE = "free_sessions";

/** What the mounted Sessions space does for the keyboard commands. */
export interface SessionsCtl {
  /** the focused session, if any */
  active(): FreeSession | undefined;
  split(dir: Dir, kind?: FreeKind): void;
  close(): void;
  stepTab(d: 1 | -1): void;
  tab(n: number): void;
  focus(dir: "left" | "right" | "up" | "down"): void;
  moveTab(d: 1 | -1): void;
  rename(): void;
  clear(): void;
  find(): void;
  restart(): void;
  toCard(): void;
}

export const sessionsCtl: { current: SessionsCtl | null } = { current: null };

/** Sessions that were asked for and have not shown up in the list yet, with where they go. */
export const placements = new Map<string, Placement>();

/** Where the session of the start call that is still out goes, until the call returns. */
export const inflight: { current: Placement | null } = { current: null };

/** The folder of the focused free session, so ⌘T opens next to it, as in a terminal app. */
let lastActiveCwd = "";
export const setActiveCwd = (cwd: string) => void (lastActiveCwd = cwd);

/**
 * Starts a free session. Without a folder it opens in the focused session's folder, else Go picks
 * the folder the last one started in, else home. split puts it beside the focused pane.
 */
export async function startFree(opts: { kind?: FreeKind; cwd?: string; name?: string; workspace_id?: string; split?: { pane: string; dir: Dir } } = {}): Promise<FreeSession> {
  const cwd = opts.cwd ?? (lastActiveCwd || undefined);
  const place: Placement = opts.split ? { split: opts.split } : {};
  inflight.current = place;
  let row: FreeSession;
  try {
    row = await api.free_session_start({ kind: opts.kind ?? "shell", ...(cwd ? { cwd } : {}), ...(opts.name ? { name: opts.name } : {}), ...(opts.workspace_id ? { workspace_id: opts.workspace_id } : {}) });
  } finally {
    inflight.current = null;
  }
  placements.set(row.id, place);
  window.dispatchEvent(new CustomEvent(FREE_STARTED, { detail: row }));
  return row;
}

/** Fired when a free session was started from this window, so the list refetches at once. */
export const FREE_STARTED = "hadron:free-started";

/** The free sessions, kept current from the change events. */
export function useFreeSessions() {
  const [list, setList] = useState<FreeSession[] | null>(null);
  const [defaultCwd, setDefaultCwd] = useState("");
  const live = useRef(true);
  const load = useCallback(async () => {
    try {
      const out = await api.free_session_list();
      if (!live.current) return;
      setList(out?.sessions ?? []);
      setDefaultCwd(out?.default_cwd ?? "");
    } catch {
      if (live.current) setList((l) => l ?? []);
    }
  }, []);
  useEffect(() => {
    live.current = true;
    void load();
    let t: ReturnType<typeof setTimeout> | undefined;
    const soon = () => {
      clearTimeout(t);
      t = setTimeout(() => void load(), 40);
    };
    const off = api.onChanged((e) => {
      if (!e.entities?.length || e.entities.includes(ENTITY_FREE)) soon();
    });
    window.addEventListener(FREE_STARTED, soon);
    return () => {
      live.current = false;
      clearTimeout(t);
      off();
      window.removeEventListener(FREE_STARTED, soon);
    };
  }, [load]);
  return { list, defaultCwd, reload: load };
}

/** The title of a session's tab: its name, else the title its program set, else the program and folder. */
export function freeTitle(s: Pick<FreeSession, "name" | "program" | "cwd" | "kind">, termTitle = ""): string {
  if (s.name) return s.name;
  if (termTitle.trim()) return termTitle.trim();
  const dir = s.cwd ? s.cwd.split("/").filter(Boolean).pop() ?? "/" : "";
  const prog = s.program || (s.kind === "claude" ? "claude" : "shell");
  return dir ? `${prog} · ${dir}` : prog;
}
