import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import { SETTING_ZOOM, clampZoom, nextZoom, zoomLabel } from "../commands";

// The zoom in force, for the terminals: they set their own font size and refit when it changes.
let current = 1;
const listeners = new Set<(z: number) => void>();
export const currentZoom = () => current;
export function onZoom(l: (z: number) => void): () => void {
  listeners.add(l);
  return () => void listeners.delete(l);
}
function publish(z: number) {
  if (z === current) return;
  current = z;
  listeners.forEach((l) => l(z));
}

/**
 * The UI zoom (the setting ui.zoom, 0.7 to 2). It reloads when settings change, so a change made in
 * Settings → Appearance or over MCP applies at once. step() moves it by one step and says so in a toast.
 */
export function useZoom(toast: (t: string) => void) {
  const [zoom, setZoom] = useState(current);
  useEffect(() => onZoom(setZoom), []);

  const load = useCallback(async () => {
    try {
      const s = await api.settings_get({ key: SETTING_ZOOM });
      if (s && SETTING_ZOOM in s) publish(clampZoom(s[SETTING_ZOOM]));
    } catch {
      // the backend is not up: the zoom stays
    }
  }, []);

  useEffect(() => {
    void load();
    return api.onChanged((e) => {
      if (!e?.entities || e.entities.includes("settings")) void load();
    });
  }, [load]);

  const set = useCallback(
    (z: number) => {
      const v = clampZoom(z);
      publish(v);
      toast(zoomLabel(v));
      void Promise.resolve()
        .then(() => api.settings_update({ values: { [SETTING_ZOOM]: v } }))
        .catch((e) => toast(e instanceof Error ? e.message : String(e)));
    },
    [toast],
  );
  const step = useCallback((dir: 1 | -1 | 0) => set(nextZoom(current, dir)), [set]);

  return { zoom, step, set };
}

/** For tests: back to 100%. */
export function resetZoomForTests() {
  current = 1;
}

/** For tests: a zoom as if the user had chosen it. */
export const publishZoomForTests = publish;
