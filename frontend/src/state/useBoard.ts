import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../api";
import type { Card, Column, LaunchProfile, Question, Role, Workspace } from "../api";
import { clearSeen, markSeen } from "./foldSeen";

export interface WsData {
  columns: Column[];
  cards: Card[];
  /** the questions still waiting for an answer */
  questions: Question[];
  /** linked-field chips by card id */
  chips: Record<string, Chip[]>;
  /** the workspace's launch profiles, for the model a card inherits */
  profiles: LaunchProfile[];
}

/** The model a card runs and where it comes from, or null when none is chosen (Claude's own default). */
export function effectiveModel(card: Card, profiles: LaunchProfile[], ws: Workspace | null): { id: string; source: "card" | "launch profile" | "workspace default" } | null {
  if (card.model) return { id: card.model, source: "card" };
  const lp = profiles.find((p) => p.name === card.launch_profile || p.id === card.launch_profile);
  if (lp?.model) return { id: lp.model, source: "launch profile" };
  if (ws?.default_model) return { id: ws.default_model, source: "workspace default" };
  return null;
}

export type Chip = { path: string; label: string; value: unknown };

export interface Toast {
  id: number;
  text: string;
}

interface State {
  ready: boolean;
  /** why the first load failed; empty once data has loaded */
  loadError: string;
  version: string;
  workspaces: Workspace[];
  activeId: string;
  data: Record<string, WsData>;
  selected: Record<string, string | null>;
  toast: Toast | null;
}

const initial: State = { ready: false, loadError: "", version: "", workspaces: [], activeId: "", data: {}, selected: {}, toast: null };

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Applies a drop to a card list the way the backend's board.MoveCard does. */
export function applyMove(cards: Card[], cardId: string, columnId: string, position?: number): Card[] {
  const card = cards.find((c) => c.id === cardId);
  if (!card) return cards;
  const from = card.column_id;
  const dest = cards.filter((c) => c.column_id === columnId && c.id !== cardId).sort((a, b) => a.position - b.position);
  const at = position === undefined || position < 0 || position > dest.length ? dest.length : position;
  dest.splice(at, 0, { ...card, column_id: columnId });
  const renumbered = new Map<string, Card>();
  dest.forEach((c, i) => renumbered.set(c.id, { ...c, position: i }));
  if (from !== columnId) {
    cards
      .filter((c) => c.column_id === from && c.id !== cardId)
      .sort((a, b) => a.position - b.position)
      .forEach((c, i) => renumbered.set(c.id, { ...c, position: i }));
  }
  return cards.map((c) => renumbered.get(c.id) ?? c);
}

export function useBoard() {
  const [s, setS] = useState<State>(initial);
  const sRef = useRef(s);
  sRef.current = s;
  const toastSeq = useRef(0);
  const alive = useRef(true);

  const toast = useCallback((text: string) => setS((p) => ({ ...p, toast: { id: ++toastSeq.current, text } })), []);
  const clearToast = useCallback(() => setS((p) => ({ ...p, toast: null })), []);

  const refetch = useCallback(async () => {
    try {
      const [workspaces, settings, status] = await Promise.all([
        api.workspace_list(),
        api.settings_get(),
        sRef.current.version ? Promise.resolve(null) : api.app_status(),
      ]);
      const entries = await Promise.all(
        workspaces.map(async (w) => {
          const [columns, cards, questions, chips, profiles] = await Promise.all([
            api.column_list({ workspace_id: w.id }),
            api.card_list({ workspace_id: w.id }),
            api.question_list({ workspace_id: w.id, status: "open" }),
            api.card_chips({ workspace_id: w.id }).catch(() => ({})),
            api.launch_profile_list({ workspace_id: w.id }).catch(() => [] as LaunchProfile[]),
          ]);
          return [w.id, { columns, cards, questions, chips: chips ?? {}, profiles: profiles ?? [] }] as const;
        }),
      );
      if (!alive.current) return;
      const stored = typeof settings.active_workspace === "string" ? settings.active_workspace : "";
      setS((p) => {
        const keep = p.activeId && workspaces.some((w) => w.id === p.activeId) ? p.activeId : "";
        const activeId = keep || (workspaces.some((w) => w.id === stored) ? stored : (workspaces[0]?.id ?? ""));
        return { ...p, ready: true, loadError: "", workspaces, activeId, data: Object.fromEntries(entries), version: status ? status.version : p.version };
      });
    } catch (e) {
      if (!alive.current) return;
      // Before the first load succeeds there is nothing to show, so the board explains and offers a retry.
      if (!sRef.current.ready) setS((p) => ({ ...p, loadError: errText(e) }));
      else toast(errText(e));
    }
  }, [toast]);

  // Initial load, and a refetch (debounced to one tick) on every `changed` event.
  useEffect(() => {
    alive.current = true;
    void refetch();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = api.onChanged(() => {
      clearTimeout(timer);
      timer = setTimeout(() => void refetch(), 30);
    });
    return () => {
      alive.current = false;
      clearTimeout(timer);
      off();
    };
  }, [refetch]);

  /** Runs a mutation, reports its error as a toast, and always reconciles with the backend. */
  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
      try {
        return await fn();
      } catch (e) {
        toast(errText(e));
        return undefined;
      } finally {
        await refetch();
      }
    },
    [refetch, toast],
  );

  const visible = useMemo(() => s.workspaces.filter((w) => !w.archived), [s.workspaces]);
  const ws = s.workspaces.find((w) => w.id === s.activeId) ?? null;
  const wsData = ws ? (s.data[ws.id] ?? { columns: [], cards: [], questions: [], chips: {}, profiles: [] }) : { columns: [] as Column[], cards: [] as Card[], questions: [] as Question[], chips: {} as Record<string, Chip[]>, profiles: [] as LaunchProfile[] };
  const selectedId = ws ? (s.selected[ws.id] ?? null) : null;
  const selected = wsData.cards.find((c) => c.id === selectedId) ?? null;

  const actions = useMemo(
    () => ({
      toast,
      clearToast,
      refetch,
      /** Runs a backend call, reports its error as a toast, and refetches. */
      run,
      switchWorkspace: (id: string) => {
        setS((p) => ({ ...p, activeId: id }));
        return run(() => api.workspace_switch({ workspace_id: id }));
      },
      createWorkspace: async (a: Parameters<typeof api.workspace_create>[0]) => {
        const w = await run(async () => {
          const created = await api.workspace_create(a);
          await api.workspace_switch({ workspace_id: created.id });
          return created;
        });
        if (w) {
          setS((p) => ({ ...p, activeId: w.id }));
          toast(`Workspace ${w.name} created.`);
        }
        return w;
      },
      renameWorkspace: (id: string, name: string) => run(() => api.workspace_update({ workspace_id: id, name })),
      /** Moves a workspace to index `to` of the tab bar, at once, then stores the order. */
      moveWorkspace: (id: string, to: number) => {
        setS((p) => {
          const live = p.workspaces.filter((w) => !w.archived && w.id !== id);
          const me = p.workspaces.find((w) => w.id === id);
          if (!me) return p;
          live.splice(Math.max(0, Math.min(to, live.length)), 0, me);
          return { ...p, workspaces: [...live, ...p.workspaces.filter((w) => w.archived && w.id !== id)] };
        });
        return run(() => api.workspace_update({ workspace_id: id, position: Math.max(0, to) }));
      },
      archiveWorkspace: async (id: string) => {
        const name = sRef.current.workspaces.find((w) => w.id === id)?.name ?? "";
        const out = await run(() => api.workspace_archive({ workspace_id: id }));
        if (out) toast(`Workspace ${name} archived. Nothing was deleted.`);
        return out;
      },
      deleteWorkspace: async (id: string) => {
        const out = await run(() => api.workspace_delete({ workspace_id: id }));
        if (out) {
          if (out.active_id) setS((p) => ({ ...p, activeId: out.active_id }));
          toast(`Workspace ${out.workspace.name} deleted.`);
        }
        return out;
      },
      select: (id: string | null) => setS((p) => (p.activeId ? { ...p, selected: { ...p.selected, [p.activeId]: id } } : p)),
      /** The user picked a card (a click, the palette, a keyboard select): show it, expanding a collapsed card panel. */
      open: (id: string) => {
        setS((p) => (p.activeId ? { ...p, selected: { ...p.selected, [p.activeId]: id } } : p));
        const cur = sRef.current;
        const w = cur.workspaces.find((x) => x.id === cur.activeId);
        if (w?.ui.collapsed?.card) void run(() => api.ui_panel_collapse({ workspace_id: cur.activeId, panel: "card", collapsed: false }));
      },
      setCollapsed: (panel: "planner" | "card", collapsed: boolean) =>
        run(() => api.ui_panel_collapse({ workspace_id: sRef.current.activeId, panel, collapsed })),
      setFullscreen: (panel: "planner" | "card" | "") => run(() => api.ui_fullscreen({ workspace_id: sRef.current.activeId, panel })),
      setMainView: (view: "board" | "dashboard") => run(() => api.ui_main_view({ workspace_id: sRef.current.activeId, view })),
      renameColumn: (id: string, name: string) => run(() => api.column_update({ column_id: id, name })),
      updateColumn: (id: string, patch: { role?: Role; wip?: number; fold?: boolean; name?: string }) =>
        run(() => api.column_update({ column_id: id, ...patch })),
      /** Collapses a column to its rail, or expands it. The rail badges the cards that arrive meanwhile. */
      collapseColumn: (id: string, on: boolean) => {
        if (on) markSeen(id, (sRef.current.data[sRef.current.activeId]?.cards ?? []).filter((c) => c.column_id === id).map((c) => c.id));
        else clearSeen(id);
        return run(() => api.column_update({ column_id: id, fold: on }));
      },
      addColumn: async (): Promise<Column | undefined> =>
        run(() => api.column_create({ workspace_id: sRef.current.activeId, name: "New column", role: "active" })),
      moveColumn: (id: string, dir: -1 | 1) => {
        const cols = sRef.current.data[sRef.current.activeId]?.columns ?? [];
        const i = cols.findIndex((c) => c.id === id);
        const j = i + dir;
        if (i < 0 || j < 0 || j >= cols.length) return Promise.resolve(undefined);
        const ids = cols.map((c) => c.id);
        [ids[i], ids[j]] = [ids[j], ids[i]];
        return run(() => api.column_reorder({ workspace_id: sRef.current.activeId, column_ids: ids }));
      },
      deleteColumn: async (id: string) => {
        const out = await run(() => api.column_delete({ column_id: id }));
        if (out) toast(`Column deleted. Its cards moved to ${out.moved_to.name}.`);
        return out;
      },
      createCard: async (a: { nick: string; key?: string; column_id?: string; launch_profile?: string; prompt?: string; model?: string; cwd?: string }) => {
        const card = await run(() => api.card_create({ workspace_id: sRef.current.activeId, ...a }));
        if (card) setS((p) => ({ ...p, selected: { ...p.selected, [p.activeId]: card.id } }));
        return card;
      },
      updateCard: (id: string, patch: { nick?: string; assignee?: string; prompt?: string; cwd?: string }) => run(() => api.card_update({ card_id: id, ...patch })),
      archiveCard: async (id: string) => {
        const out = await run(() => api.card_archive({ card_id: id }));
        if (out) setS((p) => ({ ...p, selected: { ...p.selected, [p.activeId]: null } }));
        return out;
      },
      deleteCard: async (id: string) => {
        const out = await run(() => api.card_delete({ card_id: id }));
        if (out) setS((p) => ({ ...p, selected: { ...p.selected, [p.activeId]: null } }));
        return out;
      },
      /** Optimistic: the card jumps at once, then the backend answer replaces the guess. */
      dropCard: (cardId: string, columnId: string, position?: number) => {
        setS((p) => {
          const d = p.data[p.activeId];
          if (!d) return p;
          return { ...p, data: { ...p.data, [p.activeId]: { ...d, cards: applyMove(d.cards, cardId, columnId, position) } } };
        });
        return run(() => api.card_move({ card_id: cardId, column_id: columnId, ...(position === undefined ? {} : { position }) }));
      },
    }),
    [run, toast, clearToast, refetch],
  );

  return { ...s, visible, ws, columns: wsData.columns, cards: wsData.cards, openQuestions: wsData.questions ?? [], chips: wsData.chips ?? {}, profiles: wsData.profiles ?? [], selected, actions };
}

export type Board = ReturnType<typeof useBoard>;
