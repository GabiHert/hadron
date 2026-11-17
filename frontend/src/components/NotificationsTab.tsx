import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { NotificationEvent, NotificationSettings, NotificationsOut } from "../api";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

const EVENTS: { id: NotificationEvent; label: string; hint: string }[] = [
  { id: "needs_you", label: "A card needs you", hint: "it asked a question or waits on a gate" },
  { id: "done", label: "A card is done", hint: "its session reported done" },
  { id: "blocked", label: "A card is blocked", hint: "its session reported blocked" },
  { id: "planner_reply", label: "The planner replied", hint: "only while Hadron is in the background" },
  { id: "message", label: "Messages", hint: "the notify tool, rules and integrations" },
];

const SOUNDS: { id: NotificationSettings["sound"]; label: string }[] = [
  { id: "off", label: "No sound" },
  { id: "macos", label: "The macOS notification sound" },
  { id: "hadron", label: "Hadron's chimes" },
];

/** What macOS allows, in words, and whether the user has to go to System Settings. */
export function permissionText(st: api.NotificationStatus): { text: string; fix: boolean } {
  switch (st.permission) {
    case "allowed":
      return st.alert_style === "none"
        ? { text: "macOS allows them, but banners are off: they only reach Notification Center.", fix: true }
        : { text: "macOS shows Hadron's notifications.", fix: false };
    case "denied":
      return { text: "macOS blocks Hadron's notifications.", fix: true };
    case "not_determined":
      return { text: "macOS has not asked yet. It asks the next time Hadron starts.", fix: true };
    default:
      return { text: "Notifications need Hadron.app; this build cannot show them.", fix: false };
  }
}

/** Settings → Notifications: macOS banners for what needs you, per event and per workspace. */
export function NotificationsTab({ board }: { board: Board }) {
  const { actions, workspaces } = board;
  const [n, setN] = useState<NotificationsOut | null>(null);
  const load = useCallback(async () => {
    try {
      setN(await api.notifications_settings_get());
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [actions]);
  useEffect(() => {
    void load();
    // Coming back from System Settings: read the permission again.
    const onFocus = () => void load();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [load]);
  const save = async (a: Parameters<typeof api.notifications_settings_set>[0]) => {
    try {
      setN(await api.notifications_settings_set(a));
    } catch (e) {
      actions.toast(errText(e));
    }
  };
  const test = async () => {
    try {
      const out = await api.notifications_test();
      actions.toast(out.sent ? (out.reason ? out.reason : "Test notification sent.") : `Not sent: ${out.reason ?? "unknown"}`);
    } catch (e) {
      actions.toast(errText(e));
    }
  };
  const openSystem = async () => {
    try {
      await api.notifications_open_system_settings();
    } catch (e) {
      actions.toast(errText(e));
    }
  };
  if (!n) return <h4>Notifications</h4>;
  const s = n.settings;
  const muted = s.muted_workspaces ?? [];
  const perm = permissionText(n.status);
  const live = workspaces.filter((w) => !w.archived);
  return (
    <>
      <h4>Notifications</h4>
      <p className="sub">
        Hadron shows a macOS notification when something needs you. Clicking it brings Hadron to the front and opens the card, or the planner chat. Nothing shows for the card you are already looking at.
      </p>
      <div className={"box notestatus" + (perm.fix ? " warn" : "")} role="status">
        <span>{perm.text}</span>
        <span style={{ flex: 1 }} />
        {perm.fix && <button className="btn" onClick={() => void openSystem()}>Open Notification Settings</button>}
        <button className="btn" disabled={!s.enabled} onClick={() => void test()}>Send a test</button>
      </div>
      <label className="inline" style={{ marginTop: 10 }}>
        <input type="checkbox" aria-label="Show notifications" checked={s.enabled} onChange={(e) => void save({ enabled: e.target.checked })} />
        <span>Show notifications</span>
      </label>
      <h5 style={{ margin: "14px 0 6px" }}>When</h5>
      {EVENTS.map((ev) => (
        <label key={ev.id} className="inline" style={{ marginBottom: 4 }}>
          <input type="checkbox" aria-label={ev.label} disabled={!s.enabled} checked={s.events[ev.id] !== false} onChange={(e) => void save({ events: { [ev.id]: e.target.checked } })} />
          <span>{ev.label}</span>
          <span className="hint">{ev.hint}</span>
        </label>
      ))}
      <h5 style={{ margin: "14px 0 6px" }}>Sound</h5>
      <select aria-label="Notification sound" disabled={!s.enabled} value={s.sound} onChange={(e) => void save({ sound: e.target.value as NotificationSettings["sound"] })}>
        {SOUNDS.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
      </select>
      <p className="hint" style={{ marginTop: 4 }}>Hadron already chimes when a card needs you (Settings, Sounds), so notifications are silent by default.</p>
      <h5 style={{ margin: "14px 0 6px" }}>Workspaces</h5>
      {live.map((w) => (
        <label key={w.id} className="inline" style={{ marginBottom: 4 }}>
          <input type="checkbox" aria-label={`Notify for ${w.name}`} disabled={!s.enabled} checked={!muted.includes(w.id)}
            onChange={(e) => void save({ muted_workspaces: e.target.checked ? muted.filter((x) => x !== w.id) : [...muted, w.id] })} />
          <span>{w.name}</span>
        </label>
      ))}
    </>
  );
}
