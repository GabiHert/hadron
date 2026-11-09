import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import { SETTING_KEYS, compact, parseOverrides, setActiveOverrides } from "../keys";
import type { Overrides } from "../keys";

/**
 * The user's key bindings (the setting keys.bindings), kept in step with the backend: they reload on
 * every `changed` event that touches settings, so a change made over MCP shows at once. save()
 * writes them through settings_update.
 */
export function useKeyBindings(onError?: (text: string) => void) {
  const [overrides, setOverrides] = useState<Overrides>({});
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(async () => {
    try {
      const s = await api.settings_get({ key: SETTING_KEYS });
      const o = parseOverrides(s?.[SETTING_KEYS]);
      setOverrides(o);
      setActiveOverrides(o);
    } catch {
      // No setting yet, or the backend is not up: the defaults stay.
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void load();
    return api.onChanged((e) => {
      if (!e?.entities || e.entities.includes("settings")) void load();
    });
  }, [load]);

  const save = useCallback(
    async (next: Overrides) => {
      const o = compact(next);
      setOverrides(o);
      setActiveOverrides(o);
      try {
        await api.settings_update({ values: { [SETTING_KEYS]: o } });
      } catch (e) {
        onError?.(e instanceof Error ? e.message : String(e));
        void load();
      }
    },
    [load, onError],
  );

  return { overrides, loaded, save };
}
