import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { SoundKind } from "../api";
import type { Board } from "../state/useBoard";

// The settings keys (also the planner's, through settings_update).
export const SETTING_SOUNDS_ENABLED = "sounds.enabled";
export const SETTING_SOUNDS_VOLUME = "sounds.volume";
export const SETTING_SOUNDS_MUTED = "sounds.muted_workspaces";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

export interface SoundSettings {
  enabled: boolean;
  volume: number;
  muted: string[];
}

/** The sound settings out of settings_get, with the defaults: on, at 60%, nothing muted. */
export function soundSettings(all: Record<string, unknown>): SoundSettings {
  const vol = all[SETTING_SOUNDS_VOLUME];
  const muted = all[SETTING_SOUNDS_MUTED];
  return {
    enabled: all[SETTING_SOUNDS_ENABLED] !== false,
    volume: typeof vol === "number" ? vol : 0.6,
    muted: Array.isArray(muted) ? muted.filter((x): x is string => typeof x === "string") : [],
  };
}

const SOUNDS: { kind: SoundKind; label: string; hint: string }[] = [
  { kind: "needs_you", label: "Needs you", hint: "a card moved into a needs-you column" },
  { kind: "back_to_work", label: "Back to work", hint: "a card moved out of it" },
];

/** Settings → Sounds: the chime when a card needs you and the note when it goes back to work. */
export function SoundsTab({ board }: { board: Board }) {
  const { ws, actions } = board;
  const [s, setS] = useState<SoundSettings>({ enabled: true, volume: 0.6, muted: [] });
  const load = useCallback(async () => {
    try {
      setS(soundSettings(await api.settings_get()));
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [actions]);
  useEffect(() => {
    void load();
  }, [load]);
  const save = async (values: Record<string, unknown>) => {
    try {
      setS(soundSettings(await api.settings_update({ values })));
    } catch (e) {
      actions.toast(errText(e));
    }
  };
  const preview = async (kind: SoundKind) => {
    try {
      await api.sound_preview({ sound: kind, volume: s.volume });
    } catch (e) {
      actions.toast(errText(e));
    }
  };
  const muted = !!ws && s.muted.includes(ws.id);
  return (
    <>
      <h4>Sounds</h4>
      <p className="sub">
        Hadron plays a short chime when a card moves into a needs-you column (a question, a gate, or you moved it there), and a soft note when it moves out. Several cards at once play one sound. They play for every workspace, also when the window is hidden.
      </p>
      <label className="inline">
        <input type="checkbox" aria-label="Play sounds" checked={s.enabled} onChange={(e) => void save({ [SETTING_SOUNDS_ENABLED]: e.target.checked })} />
        <span>Play sounds</span>
      </label>
      <label className="inline" style={{ marginTop: 10 }}>
        <span style={{ width: 60 }}>Volume</span>
        <input type="range" min={0} max={1} step={0.05} aria-label="Volume" disabled={!s.enabled} value={s.volume}
          onChange={(e) => setS({ ...s, volume: Number(e.target.value) })}
          onPointerUp={() => void save({ [SETTING_SOUNDS_VOLUME]: s.volume })}
          onKeyUp={() => void save({ [SETTING_SOUNDS_VOLUME]: s.volume })} />
        <span className="hint" style={{ width: 40 }}>{Math.round(s.volume * 100)}%</span>
      </label>
      <div className="box" style={{ marginTop: 12 }}>
        {SOUNDS.map((x) => (
          <div className="rrow" key={x.kind}>
            <span style={{ flex: 1 }}>
              <b style={{ fontWeight: 500 }}>{x.label}</b> <span className="hint">· {x.hint}</span>
            </span>
            <button className="btn sm" onClick={() => void preview(x.kind)} aria-label={`Preview ${x.label}`}>Play</button>
          </div>
        ))}
      </div>
      {ws ? (
        <label className="inline" style={{ marginTop: 12 }}>
          <input type="checkbox" aria-label={`Mute ${ws.name}`} checked={muted} disabled={!s.enabled}
            onChange={(e) => void save({ [SETTING_SOUNDS_MUTED]: e.target.checked ? [...s.muted, ws.id] : s.muted.filter((id) => id !== ws.id) })} />
          <span>Mute {ws.name}: its cards play no sound</span>
        </label>
      ) : null}
    </>
  );
}
