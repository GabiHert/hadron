import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { ModelOption, ModelsInfo } from "../api";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** The model list and the workspace's chosen models, reloaded by `reload`. */
export function useModels(wsId: string, onError: (t: string) => void) {
  const [info, setInfo] = useState<ModelsInfo | null>(null);
  const reload = useCallback(async () => {
    try {
      setInfo(await api.models_list(wsId ? { workspace_id: wsId } : {}));
    } catch (e) {
      onError(errText(e));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId]);
  useEffect(() => {
    void reload();
  }, [reload]);
  return { info, reload };
}

/** Options for a model select: every listed model, plus the current id when the list lacks it. */
export function ModelOptions({ models, current }: { models: ModelOption[]; current: string }) {
  return (
    <>
      {models.map((m) => <option key={m.id} value={m.id}>{m.label || m.id}</option>)}
      {current && !models.some((m) => m.id === current) ? <option value={current}>{current}</option> : null}
    </>
  );
}

const same = (a: ModelOption[], b: ModelOption[]) => a.length === b.length && a.every((m, i) => m.id === b[i].id && m.label === b[i].label);

/** Settings → Models: the model list, and this workspace's default and planner model. */
export function ModelsTab({ board }: { board: Board }) {
  const { ws, actions } = board;
  const wsId = ws?.id ?? "";
  const { info, reload } = useModels(wsId, actions.toast);
  const [rows, setRows] = useState<ModelOption[]>([]);
  const [saved, setSaved] = useState<ModelOption[]>([]);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!info) return;
    setRows(info.models.map((m) => ({ ...m })));
    setSaved(info.models);
  }, [info]);

  const save = async () => {
    setBusy(true);
    try {
      await api.models_set({ models: rows.map((m) => ({ id: m.id.trim(), label: m.label.trim() })).filter((m) => m.id) });
      actions.toast("Models saved.");
      await reload();
    } catch (e) {
      actions.toast(errText(e));
    }
    setBusy(false);
  };
  const setDefault = async (model: string) => {
    const out = await actions.run(() => api.workspace_update({ workspace_id: wsId, default_model: model }));
    if (out) await reload();
  };
  const setPlanner = async (model: string) => {
    const out = await actions.run(() => api.planner_set_model({ workspace_id: wsId, model }));
    if (out) {
      actions.toast(out.note);
      await reload();
    }
  };
  const patch = (i: number, p: Partial<ModelOption>) => setRows((r) => r.map((m, j) => (j === i ? { ...m, ...p } : m)));
  const listed = saved;
  return (
    <div className="stack">
      <h3 className="cap">Models</h3>
      {rows.map((m, i) => (
        <div className="inline" key={i}>
          <input className="in mono" aria-label="Model id" value={m.id} placeholder="opus" onChange={(e) => patch(i, { id: e.target.value })} />
          <input className="in" aria-label="Model label" value={m.label} placeholder="Opus" onChange={(e) => patch(i, { label: e.target.value })} />
          <button className="btn sm" onClick={() => setRows((r) => r.filter((_, j) => j !== i))}>Remove</button>
        </div>
      ))}
      <div className="inline">
        <button className="btn" onClick={() => setRows((r) => [...r, { id: "", label: "" }])}>Add model</button>
        <button className="btn pri" disabled={busy || same(rows, saved)} onClick={() => void save()}>Save</button>
      </div>
      <p className="hint">An id is an alias (opus, sonnet, haiku) or a full model name.</p>

      <h3 className="cap">This workspace</h3>
      <label className="field">
        <span>Default model for cards</span>
        <select className="in" aria-label="Default model for cards" value={info?.default_model ?? ""} onChange={(e) => void setDefault(e.target.value)}>
          <option value="">Claude's own default</option>
          <ModelOptions models={listed} current={info?.default_model ?? ""} />
        </select>
      </label>
      <label className="field">
        <span>Planner model</span>
        <select className="in" aria-label="Planner model" value={info?.planner_model ?? ""} onChange={(e) => void setPlanner(e.target.value)}>
          <option value="">Claude's own default</option>
          <ModelOptions models={listed} current={info?.planner_model ?? ""} />
        </select>
        <span className="hint">Typed as /model into the running planner, or used when it next starts.</span>
      </label>
      <p className="hint">A session runs its card's model, else its launch profile's, else the workspace default, else Claude's own default.</p>
    </div>
  );
}
