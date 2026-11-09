import { useEffect, useState } from "react";
import * as api from "../api";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Settings → General: app-wide behaviour that is not about one workspace. */
export function GeneralTab({ toast }: { toast: (msg: string) => void }) {
  const [restore, setRestore] = useState(true);
  useEffect(() => {
    let alive = true;
    api
      .settings_get({ key: "sessions.restore_on_launch" })
      .then((v) => alive && setRestore(v["sessions.restore_on_launch"] !== false))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);
  const toggle = async (on: boolean) => {
    try {
      await api.settings_update({ values: { "sessions.restore_on_launch": on } });
      setRestore(on);
    } catch (e) {
      toast(errText(e));
    }
  };
  return (
    <>
      <h4>General</h4>
      <label className="inline">
        <input type="checkbox" aria-label="Resume running sessions after Hadron restarts" checked={restore} onChange={(e) => void toggle(e.target.checked)} />
        <span>Resume running sessions after Hadron restarts or updates</span>
      </label>
      <p className="hint">
        Sessions that were running when Hadron quit, updated or crashed start again with their conversation. A session you stopped yourself, or that you ended with /exit, stays stopped. Turning this off also forgets what was running.
      </p>
    </>
  );
}
