import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { BlockView } from "../api";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));
const DASH_ENTITIES = ["dash", "cards", "questions", "mcp_calls", "rules", "links"];

export interface Dash {
  blocks: BlockView[];
  loaded: boolean;
  reload: () => Promise<void>;
}

/** The workspace's dashboard blocks with their data. Reloads when anything a block can read changes. */
export function useDash(workspaceId: string, onError: (text: string) => void): Dash {
  const [blocks, setBlocks] = useState<BlockView[]>([]);
  const [loaded, setLoaded] = useState(false);
  const seq = useRef(0);
  const errRef = useRef(onError);
  errRef.current = onError;

  const load = useCallback(async () => {
    if (!workspaceId) return;
    const mine = ++seq.current;
    try {
      const out = await api.dash_get({ workspace_id: workspaceId });
      if (mine !== seq.current) return;
      setBlocks(out?.blocks ?? []);
      setLoaded(true);
    } catch (e) {
      if (mine === seq.current) errRef.current(errText(e));
    }
  }, [workspaceId]);

  useEffect(() => {
    setBlocks([]);
    setLoaded(false);
    void load();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = api.onChanged((e) => {
      if (e.entities?.length && !e.entities.some((x) => DASH_ENTITIES.includes(x))) return;
      clearTimeout(timer);
      timer = setTimeout(() => void load(), 30);
    });
    return () => {
      seq.current++;
      clearTimeout(timer);
      off();
    };
  }, [load]);

  return { blocks, loaded, reload: load };
}

/** The `widgets.scripts` setting: true only when it is exactly true. Refetched when settings change. */
export function useWidgetScripts(enabled = true): boolean {
  const [on, setOn] = useState(false);
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    const load = () =>
      api
        .settings_get({ key: "widgets.scripts" })
        .then((s) => alive && setOn(s?.["widgets.scripts"] === true))
        .catch(() => alive && setOn(false));
    void load();
    const off = api.onChanged((e) => {
      if (!e.entities?.length || e.entities.includes("settings")) void load();
    });
    return () => {
      alive = false;
      off();
    };
  }, [enabled]);
  return on;
}

/** The ids of chat messages already pinned to the dashboard, read from blocks' source.from. */
export function usePinnedIds(workspaceId: string, enabled: boolean): Set<string> {
  const [ids, setIds] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!enabled || !workspaceId) return;
    let alive = true;
    const load = () =>
      api
        .dash_get({ workspace_id: workspaceId })
        .then((o) => alive && setIds(new Set((o?.blocks ?? []).map((b) => b.source?.from).filter((x): x is string => typeof x === "string" && x !== ""))))
        .catch(() => {});
    void load();
    const off = api.onChanged((e) => {
      if (!e.entities?.length || e.entities.includes("dash")) void load();
    });
    return () => {
      alive = false;
      off();
    };
  }, [workspaceId, enabled]);
  return ids;
}
