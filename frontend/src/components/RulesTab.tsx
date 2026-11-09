import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { DryRunItem, FieldEntry, Rule, RuleLogEntry } from "../api";
import { ACTIONS, REPORT_KINDS, TRIGGERS, buildRule, celOfRule, conditionOf, draftProblem, newDraft } from "../rulebuilder";
import type { RuleDraft } from "../rulebuilder";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Settings → Rules: the When / If / Then builder, the rule list with on/off switches, the dry run and the log. */
export function RulesTab({ board }: { board: Board }) {
  const { ws, columns, actions } = board;
  const wsId = ws?.id ?? "";
  const [rules, setRules] = useState<Rule[]>([]);
  const [schema, setSchema] = useState<FieldEntry[]>([]);
  const [log, setLog] = useState<RuleLogEntry[]>([]);
  const [draft, setDraft] = useState<RuleDraft>(() => newDraft(columns, []));
  const [dry, setDry] = useState<DryRunItem[] | null>(null);
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    if (!wsId) return;
    try {
      const [r, s, l] = await Promise.all([api.rule_list({ workspace_id: wsId }), api.field_schema_get({ workspace_id: wsId }), api.rule_log({ workspace_id: wsId, limit: 12 })]);
      setRules(r);
      setSchema(s.fields);
      setLog(l);
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [wsId, actions]);

  useEffect(() => {
    void reload();
  }, [reload]);
  useEffect(() => api.onChanged((e) => { if (e.workspace_id === wsId && e.entities.some((x) => x === "rules" || x === "messages")) void reload(); }), [wsId, reload]);

  // The first schema load fills in the field dropdowns.
  const loaded = schema.length > 0;
  useEffect(() => {
    if (loaded) setDraft((d) => (d.field ? d : newDraft(columns, schema)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded]);

  const set = (patch: Partial<RuleDraft>) => setDraft((d) => ({ ...d, ...patch }));
  const field = (p: string) => schema.find((f) => f.path === p);
  const ifEntry = field(draft.ifField);
  const problem = draftProblem(draft);
  const payload = buildRule(draft, columns);

  const create = async () => {
    setError("");
    try {
      await api.rule_create({ workspace_id: wsId, ...payload });
      setDraft((d) => ({ ...d, name: "", text: "", setValue: "" }));
      actions.toast(`Rule ${payload.name} created.`);
      await reload();
    } catch (e) {
      setError(errText(e));
    }
  };
  // The backend also emits `changed`; reloading here keeps the list right even if that event is late.
  const mutate = async (fn: () => Promise<unknown>) => {
    await actions.run(fn);
    await reload();
  };
  const toggle = (r: Rule) => mutate(() => api.rule_update({ rule_id: r.id, enabled: !r.enabled }));
  const remove = (r: Rule) => mutate(() => api.rule_delete({ rule_id: r.id }));
  const move = (i: number, dir: -1 | 1) => {
    const ids = rules.map((r) => r.id);
    const j = i + dir;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    return mutate(() => api.rule_reorder({ workspace_id: wsId, rule_ids: ids }));
  };
  const dryRun = async () => {
    try {
      const items = await api.rule_dry_run({ workspace_id: wsId });
      setDry(items);
      actions.toast(items.filter((x) => !x.skipped).length ? `${items.filter((x) => !x.skipped).length} change(s) would happen if the rules ran now.` : "Nothing would change. The board matches the rules.");
    } catch (e) {
      actions.toast(errText(e));
    }
  };

  const editable = schema.filter((f) => f.editable);
  const colSelect = (id: string, value: string, onChange: (v: string) => void, label: string) => (
    <select id={id} className="in" value={value} onChange={(e) => onChange(e.target.value)} aria-label={label}>
      {columns.map((c) => (
        <option key={c.id} value={c.id}>
          {c.name}
        </option>
      ))}
    </select>
  );

  return (
    <>
      <div className="inline">
        <h4 style={{ flex: 1 }}>Rules</h4>
        <button className="btn" onClick={() => void dryRun()}>
          Dry run on the board
        </button>
      </div>
      <p className="sub">
        Rules read typed fields. Any integration can add fields, so a rule can watch Jira, Linear or your own fields. The planner can create these too: try "when jira.status is Finished move to Finished" in its chat.
      </p>
      <div className="rulecard">
        <div className="rulegrid">
          <span className="kw">Name</span>
          <input id="nr-name" className="in" value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder="Finished tickets go to Done" aria-label="Rule name" />

          <span className="kw">When</span>
          <div className="inline">
            <select id="nr-on" className="in" value={draft.on} onChange={(e) => set({ on: e.target.value as RuleDraft["on"] })} aria-label="When">
              {TRIGGERS.map((t) => (
                <option key={t.on} value={t.on}>
                  {t.label}
                </option>
              ))}
            </select>
            {draft.on === "field_changed" ? (
              <select id="nr-field" className="in" value={draft.field} onChange={(e) => set({ field: e.target.value, ifField: e.target.value, ifValue: field(e.target.value)?.options?.[0] ?? "" })} aria-label="Field to watch">
                {schema.map((f) => (
                  <option key={f.path} value={f.path}>
                    {f.path}
                  </option>
                ))}
              </select>
            ) : null}
            {draft.on === "column_entered" ? colSelect("nr-column", draft.column, (v) => set({ column: v }), "Column entered") : null}
            {draft.on === "report" ? (
              <select className="in" value={draft.kind} onChange={(e) => set({ kind: e.target.value })} aria-label="Report kind">
                <option value="">any report</option>
                {REPORT_KINDS.map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            ) : null}
            {draft.on === "idle_for" ? (
              <>
                <input className="in" type="number" min={1} style={{ width: 70 }} value={draft.minutes} onChange={(e) => set({ minutes: e.target.value })} aria-label="Idle minutes" />
                <span className="hint">minutes</span>
              </>
            ) : null}
            {draft.on === "no_report_for" ? (
              <>
                <input className="in" type="number" min={1} style={{ width: 70 }} value={draft.minutes} onChange={(e) => set({ minutes: e.target.value })} aria-label="Quiet minutes" />
                <span className="hint">minutes without a report</span>
              </>
            ) : null}
            {draft.on === "cron" ? <input className="in mono" style={{ width: 150 }} value={draft.spec} onChange={(e) => set({ spec: e.target.value })} aria-label="Cron spec" placeholder="0 9 * * 1-5" /> : null}
          </div>

          <span className="kw">If</span>
          <div className="inline">
            <select id="nr-if-field" className="in" value={draft.ifField} onChange={(e) => set({ ifField: e.target.value, ifValue: field(e.target.value)?.options?.[0] ?? "" })} aria-label="Condition field">
              <option value="">always</option>
              {schema.map((f) => (
                <option key={f.path} value={f.path}>
                  {f.path}
                </option>
              ))}
            </select>
            {draft.ifField ? (
              <>
                <select id="nr-op" className="in" value={draft.ifOp} onChange={(e) => set({ ifOp: e.target.value as "is" | "not" })} aria-label="Condition operator">
                  <option value="is">is</option>
                  <option value="not">is not</option>
                </select>
                {ifEntry?.options?.length ? (
                  <select id="nr-val" className="in" value={draft.ifValue} onChange={(e) => set({ ifValue: e.target.value })} aria-label="Condition value">
                    {ifEntry.options.map((o) => (
                      <option key={o} value={o}>
                        {o}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input id="nr-val" className="in" value={draft.ifValue} onChange={(e) => set({ ifValue: e.target.value })} aria-label="Condition value" />
                )}
              </>
            ) : null}
          </div>

          <span className="kw">Then</span>
          <div className="inline">
            <select id="nr-action" className="in" value={draft.action} onChange={(e) => set({ action: e.target.value as RuleDraft["action"] })} aria-label="Then">
              {ACTIONS.map((a) => (
                <option key={a.type} value={a.type}>
                  {a.label}
                </option>
              ))}
            </select>
            {draft.action === "move" ? colSelect("nr-col", draft.toColumn, (v) => set({ toColumn: v }), "Move to column") : null}
            {draft.action === "set_field" ? (
              <>
                <select className="in" value={draft.setPath} onChange={(e) => set({ setPath: e.target.value })} aria-label="Field to set">
                  {editable.map((f) => (
                    <option key={f.path} value={f.path}>
                      {f.path}
                    </option>
                  ))}
                </select>
                <span className="hint">to</span>
                <input className="in" value={draft.setValue} onChange={(e) => set({ setValue: e.target.value })} aria-label="Value to set" />
              </>
            ) : null}
            {draft.action === "notify" || draft.action === "tell_planner" ? (
              <input className="in" style={{ flex: 1, minWidth: 160 }} value={draft.text} onChange={(e) => set({ text: e.target.value })} aria-label="Message" placeholder="{{card.nick}} is ready" />
            ) : null}
            {draft.on === "no_report_for" && (draft.action === "notify" || draft.action === "tell_planner") ? (
              <span className="hint" aria-label="Variables">Variables: {"{{card.nick}}"}, and for this trigger {"{{quiet.minutes}}"}, {"{{report.last}}"}, {"{{report.last_kind}}"}, {"{{report.last_ago}}"}</span>
            ) : null}
          </div>
        </div>
        <div className="inline" style={{ padding: "0 14px 12px" }}>
          <span className="cel" style={{ flex: 1 }} aria-label="Rule as CEL">
            {celOfRule({ trigger: payload.trigger, condition: conditionOf(draft), actions: payload.actions }, columns)}
          </span>
          <button className="btn pri" disabled={!!problem} title={problem} onClick={() => void create()}>
            Create rule
          </button>
        </div>
        {error ? (
          <div className="hint" role="alert" style={{ padding: "0 14px 12px", color: "var(--bad)", whiteSpace: "pre-wrap" }}>
            {error}
          </div>
        ) : null}
      </div>

      <div className="box">
        {rules.map((r, i) => (
          <div key={r.id} className="rrow">
            <button className={"switch" + (r.enabled ? " on" : "")} onClick={() => void toggle(r)} aria-label={(r.enabled ? "Turn off " : "Turn on ") + r.name} aria-pressed={r.enabled} />
            <span style={{ flex: 1, minWidth: 0 }}>
              <b style={{ fontWeight: 500 }}>{r.name}</b> {r.builtin ? <span className="pill ok">built-in</span> : null}
              <br />
              <span className="cel">{celOfRule(r, columns)}</span>
            </span>
            <button className="btn sm" onClick={() => void move(i, -1)} disabled={i === 0} aria-label={`Move ${r.name} up`}>
              ↑
            </button>
            <button className="btn sm" onClick={() => void move(i, 1)} disabled={i === rules.length - 1} aria-label={`Move ${r.name} down`}>
              ↓
            </button>
            {r.builtin ? null : (
              <button className="btn sm quiet" onClick={() => void remove(r)}>
                Delete
              </button>
            )}
          </div>
        ))}
        {!rules.length ? <div className="rrow hint">No rules in {ws?.name} yet.</div> : null}
      </div>

      {dry ? (
        <div className="box" aria-label="Dry run result">
          <div className="boxh">
            <b>Dry run</b>
            <span className="hint">nothing was applied</span>
            <div className="spacer" />
            <button className="btn sm quiet" onClick={() => setDry(null)}>
              Close
            </button>
          </div>
          {dry.map((d, i) => (
            <div key={i} className="rrow">
              <span className="mono">{d.card}</span>
              <span style={{ flex: 1 }}>
                {d.skipped ? <span className="hint">skipped by {d.rule}: {d.reason}</span> : <>{d.rule}: {d.action} {d.from ? `${d.from} → ` : ""}{d.to}</>}
              </span>
            </div>
          ))}
          {!dry.length ? <div className="rrow hint">No card would change.</div> : null}
        </div>
      ) : null}

      {log.length ? (
        <>
          <div className="cap">Recent firings</div>
          <div className="box">
            {log.map((l) => (
              <div key={l.id} className="rrow">
                <span className={"pill " + (l.kind === "skip" ? "warn" : "ok")}>{l.kind}</span>
                <span className="mono">{l.card}</span>
                <span style={{ flex: 1 }}>{l.rule}{l.reason ? <span className="hint"> · {l.reason}</span> : null}</span>
              </div>
            ))}
          </div>
        </>
      ) : null}
    </>
  );
}
