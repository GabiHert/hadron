import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../api";
import type { Card, FieldEntry, View } from "../api";
import { EMPTY_FILTER, fromSpec, isActive, localMatch, needsServer, setVals, toSpec } from "../filters";
import type { Filter } from "../filters";
import type { Board } from "./useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/**
 * The filter bar's state for the active workspace: the filter, the field schema, the saved views,
 * and which cards pass. Pinned values and the search run in the browser; an expression, or a value
 * on an integration's field, goes to the backend's filter_eval.
 */
export function useFilters(board: Board) {
  const { ws, cards, columns, actions } = board;
  const wsId = ws?.id ?? "";
  const [byWs, setByWs] = useState<Record<string, Filter>>({});
  const filter = byWs[wsId] ?? EMPTY_FILTER;
  const [schema, setSchema] = useState<FieldEntry[]>([]);
  const [views, setViews] = useState<View[]>([]);
  const [ids, setIds] = useState<Set<string> | null>(null);
  const [error, setError] = useState("");
  const seq = useRef(0);

  const setFilter = useCallback(
    (patch: Partial<Filter>) => setByWs((p) => ({ ...p, [wsId]: { ...(p[wsId] ?? EMPTY_FILTER), view: null, ...patch } })),
    [wsId],
  );

  const reload = useCallback(async () => {
    if (!wsId) return;
    try {
      const [s, v] = await Promise.all([api.field_schema_get({ workspace_id: wsId }), api.view_list({ workspace_id: wsId })]);
      setSchema(s.fields);
      setViews(v);
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [wsId, actions]);

  const integrations = ws?.integrations.join(",") ?? "";
  useEffect(() => {
    void reload();
  }, [reload, integrations]);
  useEffect(() => {
    const off = api.onChanged((e) => {
      if (e.workspace_id === wsId && e.entities.some((x) => x === "views" || x === "fields")) void reload();
    });
    return off;
  }, [wsId, reload]);

  // The backend evaluates an expression, or a value on an integration's field. It runs again when the cards change.
  const server = needsServer(filter);
  const cardSig = cards.map((c) => `${c.id}:${c.column_id}:${c.assignee}:${c.stage}`).join("|");
  useEffect(() => {
    if (!server) {
      setIds(null);
      setError("");
      return;
    }
    const mine = ++seq.current;
    const t = setTimeout(() => {
      api
        .filter_eval({ workspace_id: wsId, vals: setVals(filter), q: filter.q, expr: filter.expr })
        .then((r) => {
          if (mine !== seq.current) return;
          setIds(new Set(r.card_ids));
          setError("");
        })
        .catch((e) => {
          if (mine !== seq.current) return;
          setIds(null);
          setError(errText(e));
        });
    }, 150);
    return () => clearTimeout(t);
    // The filter object changes identity on every render of the parent; its parts are what matter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server, wsId, JSON.stringify(setVals(filter)), filter.q, filter.expr, cardSig]);

  const visible = useCallback(
    (card: Card) => {
      if (!isActive(filter)) return true;
      if (server) return ids ? ids.has(card.id) : true;
      return localMatch(card, columns, filter);
    },
    [filter, server, ids, columns],
  );

  const pinned = useMemo(() => (ws?.filter_bar ?? []).filter((p) => schema.some((f) => f.path === p)), [ws?.filter_bar, schema]);

  return {
    filter,
    schema,
    views,
    pinned,
    /** the backend's complaint about the expression, verbatim */
    error,
    active: isActive(filter),
    visible,
    setVal: (path: string, value: string) => setFilter({ vals: { ...filter.vals, [path]: value } }),
    setQ: (q: string) => setFilter({ q }),
    setExpr: (expr: string) => setFilter({ expr }),
    clear: () => setFilter({ vals: {}, q: "", expr: "" }),
    /** Asks the backend whether an expression compiles. Resolves to its message, or "". */
    check: async (expr: string): Promise<string> => {
      if (!expr.trim()) return "";
      try {
        await api.filter_eval({ workspace_id: wsId, expr });
        return "";
      } catch (e) {
        return errText(e);
      }
    },
    pin: (path: string) => {
      const cur = ws?.filter_bar ?? [];
      const next = cur.includes(path) ? cur.filter((p) => p !== path) : [...cur, path];
      if (cur.includes(path)) {
        const { [path]: _gone, ...rest } = filter.vals;
        void _gone;
        setFilter({ vals: rest });
      }
      return actions.run(() => api.filter_bar_set({ workspace_id: wsId, fields: next }));
    },
    saveView: async (name: string) => {
      const v = await actions.run(() => api.view_save({ workspace_id: wsId, name, filters: toSpec(filter) }));
      await reload();
      if (v) {
        setByWs((p) => ({ ...p, [wsId]: { ...(p[wsId] ?? EMPTY_FILTER), view: v.id } }));
        actions.toast(`Saved view ${v.name}.`);
      }
      return v;
    },
    applyView: (v: View) => setByWs((p) => ({ ...p, [wsId]: fromSpec(v.filters, v.id) })),
    deleteView: async (id: string) => {
      await actions.run(() => api.view_delete({ view_id: id }));
      if (filter.view === id) setFilter({});
      await reload();
    },
  };
}

export type Filters = ReturnType<typeof useFilters>;
