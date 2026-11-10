import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { Routine } from "../api";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

export type When = "weekdays" | "daily" | "every" | "cron";

export interface RoutineDraft {
  id: string | null;
  name: string;
  when: When;
  /** HH:MM, for weekdays and daily */
  time: string;
  every: string;
  unit: "minutes" | "hours";
  cron: string;
  /** "planner" or a card id */
  target: string;
  prompt: string;
  catchUp: boolean;
}

export const newRoutineDraft = (): RoutineDraft => ({ id: null, name: "", when: "weekdays", time: "09:00", every: "30", unit: "minutes", cron: "0 9 * * 1-5", target: "planner", prompt: "", catchUp: true });

const pad = (n: number) => String(n).padStart(2, "0");

/** The schedule a draft asks for, as routine_create's cron or every_minutes. */
export function scheduleOf(d: RoutineDraft): { cron?: string; every_minutes?: number } | string {
  if (d.when === "weekdays" || d.when === "daily") {
    const m = /^(\d{1,2}):(\d{2})$/.exec(d.time.trim());
    if (!m || +m[1] > 23 || +m[2] > 59) return "Pick a time such as 09:00.";
    return { cron: `${+m[2]} ${+m[1]} * * ${d.when === "weekdays" ? "1-5" : "*"}` };
  }
  if (d.when === "every") {
    const n = parseInt(d.every, 10);
    if (!(n > 0)) return "Say how often, such as every 30 minutes.";
    const minutes = d.unit === "hours" ? n * 60 : n;
    if (minutes < 5) return "A routine runs at most every 5 minutes.";
    return { every_minutes: minutes };
  }
  return d.cron.trim() ? { cron: d.cron.trim() } : "Write a cron spec, such as 0 9 * * 1-5.";
}

/** A routine's schedule in words: "Weekdays at 09:00", "Every 2 hours", or its cron spec. */
export function describeSchedule(r: Pick<Routine, "cron" | "every_minutes">): string {
  if (r.every_minutes) {
    const n = r.every_minutes;
    if (n % 60 === 0) return n === 60 ? "Every hour" : `Every ${n / 60} hours`;
    return `Every ${n} minutes`;
  }
  const m = /^(\d{1,2}) (\d{1,2}) \* \* (\*|1-5)$/.exec(r.cron.trim());
  if (m) return `${m[3] === "*" ? "Every day" : "Weekdays"} at ${pad(+m[2])}:${pad(+m[1])}`;
  return `cron ${r.cron}`;
}

/** The draft that edits a routine. */
export function draftOf(r: Routine): RoutineDraft {
  const d: RoutineDraft = { ...newRoutineDraft(), id: r.id, name: r.name, prompt: r.prompt, catchUp: r.catch_up, target: r.target === "card" ? r.card_id : "planner" };
  if (r.every_minutes) {
    const hours = r.every_minutes % 60 === 0;
    return { ...d, when: "every", every: String(hours ? r.every_minutes / 60 : r.every_minutes), unit: hours ? "hours" : "minutes" };
  }
  const m = /^(\d{1,2}) (\d{1,2}) \* \* (\*|1-5)$/.exec(r.cron.trim());
  if (m) return { ...d, when: m[3] === "*" ? "daily" : "weekdays", time: `${pad(+m[2])}:${pad(+m[1])}` };
  return { ...d, when: "cron", cron: r.cron };
}

const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : "");

/** Settings → Routines: prompts sent to the planner or a card's session on a schedule. */
export function RoutinesTab({ board }: { board: Board }) {
  const { ws, cards, actions } = board;
  const wsId = ws?.id ?? "";
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [draft, setDraft] = useState<RoutineDraft>(newRoutineDraft);
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    if (!wsId) return;
    try {
      setRoutines(await api.routine_list({ workspace_id: wsId }));
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [wsId, actions]);
  useEffect(() => {
    void reload();
  }, [reload]);
  useEffect(() => api.onChanged((e) => { if (e.workspace_id === wsId && e.entities.includes("routines")) void reload(); }), [wsId, reload]);

  const set = (patch: Partial<RoutineDraft>) => setDraft((d) => ({ ...d, ...patch }));
  const sched = scheduleOf(draft);
  const problem = !draft.name.trim() ? "Give the routine a name." : typeof sched === "string" ? sched : !draft.prompt.trim() ? "Write the prompt it sends." : "";
  const cardName = (id: string) => cards.find((c) => c.id === id)?.nick ?? "a card that is gone";
  const targetOf = (r: Routine) => (r.target === "planner" ? "the planner" : cardName(r.card_id));

  const save = async () => {
    if (problem || typeof sched === "string") return;
    setError("");
    const target = draft.target === "planner" ? { target: "planner" as const } : { target: "card" as const, card: draft.target };
    const body = { name: draft.name.trim(), prompt: draft.prompt, catch_up: draft.catchUp, ...target };
    try {
      if (draft.id) {
        // Send both schedule fields so switching between cron and every_minutes clears the other.
        await api.routine_update({ routine_id: draft.id, ...body, cron: sched.cron ?? "", every_minutes: sched.every_minutes ?? 0 });
        actions.toast(`Routine ${body.name} saved.`);
      } else {
        await api.routine_create({ workspace_id: wsId, ...body, ...sched });
        actions.toast(`Routine ${body.name} created.`);
      }
      setDraft(newRoutineDraft());
      await reload();
    } catch (e) {
      setError(errText(e));
    }
  };
  const mutate = async (fn: () => Promise<unknown>) => {
    await actions.run(fn);
    await reload();
  };
  const toggle = (r: Routine) => mutate(() => api.routine_update({ routine_id: r.id, enabled: !r.enabled }));
  const remove = (r: Routine) => mutate(() => api.routine_delete({ routine_id: r.id }));
  const runNow = (r: Routine) =>
    mutate(async () => {
      const out = await api.routine_run({ routine_id: r.id });
      actions.toast(`${r.name}: ${out.last_result}.`);
    });

  return (
    <>
      <h4>Routines</h4>
      <p className="sub">
        A routine types a prompt into the planner or a card's session on a schedule. A stopped session is started and gets the prompt once Claude is ready. Times are your Mac's. The planner can manage these too: try "every weekday at 9 ask me for a standup" in its chat.
      </p>

      <div className="rulecard" aria-label={draft.id ? "Edit routine" : "New routine"}>
        <div className="rulegrid">
          <span className="kw">Name</span>
          <input className="in" value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder="Morning standup" aria-label="Routine name" />

          <span className="kw">When</span>
          <div className="inline">
            <select className="in" value={draft.when} onChange={(e) => set({ when: e.target.value as When })} aria-label="When">
              <option value="weekdays">Weekdays at</option>
              <option value="daily">Every day at</option>
              <option value="every">Every</option>
              <option value="cron">Cron spec</option>
            </select>
            {draft.when === "weekdays" || draft.when === "daily" ? <input className="in mono" type="time" style={{ width: 110 }} value={draft.time} onChange={(e) => set({ time: e.target.value })} aria-label="Time" /> : null}
            {draft.when === "every" ? (
              <>
                <input className="in" type="number" min={1} style={{ width: 70 }} value={draft.every} onChange={(e) => set({ every: e.target.value })} aria-label="How often" />
                <select className="in" value={draft.unit} onChange={(e) => set({ unit: e.target.value as RoutineDraft["unit"] })} aria-label="Unit">
                  <option value="minutes">minutes</option>
                  <option value="hours">hours</option>
                </select>
              </>
            ) : null}
            {draft.when === "cron" ? <input className="in mono" style={{ width: 150 }} value={draft.cron} onChange={(e) => set({ cron: e.target.value })} aria-label="Cron spec" placeholder="0 9 * * 1-5" /> : null}
          </div>

          <span className="kw">To</span>
          <select className="in" value={draft.target} onChange={(e) => set({ target: e.target.value })} aria-label="Send to">
            <option value="planner">The planner</option>
            {cards.map((c) => (
              <option key={c.id} value={c.id}>
                {c.key ? `${c.key} ${c.nick}` : c.nick}
              </option>
            ))}
          </select>

          <span className="kw">Prompt</span>
          <textarea className="in" rows={3} value={draft.prompt} onChange={(e) => set({ prompt: e.target.value })} aria-label="Prompt" placeholder="Post a standup: what moved since yesterday, what is blocked." />

          <span className="kw">Missed</span>
          <label className="inline hint">
            <input type="checkbox" checked={draft.catchUp} onChange={(e) => set({ catchUp: e.target.checked })} aria-label="Run once when Hadron opens if it was closed at the time" />
            If Hadron was closed at the time, run once when it opens
          </label>
        </div>
        <div className="inline" style={{ padding: "0 14px 12px", justifyContent: "flex-end" }}>
          {draft.id ? (
            <button className="btn" onClick={() => setDraft(newRoutineDraft())}>
              Cancel
            </button>
          ) : null}
          <button className="btn pri" disabled={!!problem} title={problem} onClick={() => void save()}>
            {draft.id ? "Save routine" : "Create routine"}
          </button>
        </div>
        {error ? (
          <div className="hint" role="alert" style={{ padding: "0 14px 12px", color: "var(--bad)", whiteSpace: "pre-wrap" }}>
            {error}
          </div>
        ) : null}
      </div>

      <div className="box" aria-label="Routines">
        {routines.map((r) => (
          <div key={r.id} className="rrow">
            <button className={"switch" + (r.enabled ? " on" : "")} onClick={() => void toggle(r)} aria-label={(r.enabled ? "Pause " : "Resume ") + r.name} aria-pressed={r.enabled} />
            <span style={{ flex: 1, minWidth: 0 }}>
              <b style={{ fontWeight: 500 }}>{r.name}</b> <span className="hint">· {describeSchedule(r)} · to {targetOf(r)}</span>
              <br />
              <span className="hint" data-testid={`routine-status-${r.id}`}>
                {r.enabled ? (r.next_run_at ? `Next: ${when(r.next_run_at)}` : "") : "Paused"}
                {r.last_result ? ` · Last${r.last_run_at ? ` (${when(r.last_run_at)})` : ""}: ${r.last_result}` : ""}
              </span>
            </span>
            <button className="btn sm" onClick={() => void runNow(r)} aria-label={`Run ${r.name} now`}>
              Run now
            </button>
            <button className="btn sm" onClick={() => setDraft(draftOf(r))} aria-label={`Edit ${r.name}`}>
              Edit
            </button>
            <button className="btn sm quiet" onClick={() => void remove(r)} aria-label={`Delete ${r.name}`}>
              Delete
            </button>
          </div>
        ))}
        {!routines.length ? <div className="rrow hint">No routines in {ws?.name} yet.</div> : null}
      </div>
    </>
  );
}
