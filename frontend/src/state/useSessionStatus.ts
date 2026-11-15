import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { SessionStatus } from "../api";

/**
 * A session's status, fetched on mount and again on every `changed` event (the backend emits one
 * whenever a session starts, ends or changes liveness).
 */
const STATUS_ENTITIES = new Set(["sessions", "cards", "questions", "settings", "launch_profiles", "workspaces"]);

export function useSessionStatus(fetchStatus: () => Promise<SessionStatus | null>, deps: unknown[]): SessionStatus | null {
  const [status, setStatus] = useState<SessionStatus | null>(null);
  const fetchRef = useRef(fetchStatus);
  fetchRef.current = fetchStatus;
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setStatus(null);
    const load = () =>
      fetchRef
        .current()
        .then((s) => alive && setStatus(s))
        .catch(() => {});
    void load();
    const off = api.onChanged((e) => {
      // A session's status changes with its session, its card or an open question; the MCP call
      // log and chat messages, which most events are about, leave it as it was.
      if (e?.entities?.length && !e.entities.some((x) => STATUS_ENTITIES.has(x))) return;
      clearTimeout(timer);
      timer = setTimeout(() => void load(), 40);
    });
    return () => {
      alive = false;
      clearTimeout(timer);
      off();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return status;
}
