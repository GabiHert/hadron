import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { PlannerPolicy, PlannerPolicyInfo } from "../api";
import type { Board } from "../state/useBoard";
import { useSessionStatus } from "../state/useSessionStatus";
import { AdoptPlanner } from "./AdoptPlanner";
import { ModelOptions, useModels } from "./ModelsTab";
import { ProfileGrant, ProfileOptions, useProfiles } from "./ProfileGrant";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

export const POLICY_TEXT: Record<PlannerPolicy, string> = {
  allow_all: "The planner may answer every question and clear every gate itself, then tell you.",
  decisions_only: "The planner may answer decisions and unblocks; approvals come to you.",
  ask_for_prd: "The planner may answer approvals that target DEV; anything that targets PRD comes to you.",
  user_only: "The planner answers and clears nothing itself; it asks you, then relays what you said in chat.",
};
const POLICIES: PlannerPolicy[] = ["allow_all", "decisions_only", "ask_for_prd", "user_only"];

/** Settings → Planner: this workspace's planner profile, policy, model and lifecycle. User-only. */
export function PlannerTab({ board }: { board: Board }) {
  const { ws, actions } = board;
  const wsId = ws?.id ?? "";
  const [adoptOpen, setAdoptOpen] = useState(false);
  const [policy, setPolicy] = useState<PlannerPolicyInfo | null>(null);
  const [autostart, setAutostart] = useState(true);
  const [rev, setRev] = useState(0);
  const profiles = useProfiles(wsId, actions.toast);
  const { info: modelInfo, reload: reloadModels } = useModels(wsId, actions.toast);
  const status = useSessionStatus(() => (wsId ? api.planner_get({ workspace_id: wsId }) : Promise.resolve(null)), [wsId, rev]);
  const [profile, setProfile] = useState("");
  useEffect(() => {
    if (status) setProfile(status.launch_profile ?? "");
  }, [status]);

  const loadPolicy = useCallback(async () => {
    try {
      setPolicy(await api.planner_policy_get({ workspace_id: wsId }));
    } catch (e) {
      actions.toast(errText(e));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId]);
  useEffect(() => {
    void loadPolicy();
  }, [loadPolicy]);
  useEffect(() => {
    let alive = true;
    api
      .settings_get({ key: "planner.autostart" })
      .then((v) => alive && setAutostart(v["planner.autostart"] !== false))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  if (!ws) return null;
  const chosen = profiles.find((p) => p.name === profile);
  const own: string = policy?.override ?? "inherit";
  const setProfileTo = async (name: string) => {
    const out = await actions.run(() => api.planner_set_profile({ workspace_id: wsId, launch_profile: name }));
    if (!out) return;
    setProfile(name);
    actions.toast(name ? `Planner launch profile set to ${name}.` : "Planner launch profile cleared.");
    setRev((r) => r + 1);
  };
  const setPolicyTo = async (value: string) => {
    const out = await actions.run(() => api.planner_policy_set({ workspace_id: wsId, policy: value as PlannerPolicy | "inherit" }));
    if (!out) return;
    setPolicy(out);
    actions.toast("Planner policy saved.");
  };
  const setModel = async (model: string) => {
    const out = await actions.run(() => api.planner_set_model({ workspace_id: wsId, model }));
    if (out) {
      actions.toast(out.note);
      await reloadModels();
    }
  };
  const toggleAutostart = async (on: boolean) => {
    try {
      await api.settings_update({ values: { "planner.autostart": on } });
      setAutostart(on);
      actions.toast(on ? "The planner starts when Hadron opens." : "The planner no longer starts when Hadron opens.");
    } catch (e) {
      actions.toast(errText(e));
    }
  };
  const life = async (fn: () => Promise<unknown>, done: string) => {
    const out = await actions.run(fn);
    if (out) {
      actions.toast(done);
      setRev((r) => r + 1);
    }
  };
  const running = !!status?.running;
  return (
    <div className="stack">
      <h3 className="cap">Planner · {ws.name}</h3>

      <h3 className="cap">Launch profile</h3>
      <label className="field">
        <span>Launch profile</span>
        <select className="in" aria-label="Planner launch profile" value={profile} onChange={(e) => void setProfileTo(e.target.value)}>
          <ProfileOptions profiles={profiles} current={profile} />
        </select>
      </label>
      <ProfileGrant profile={chosen} />
      <p className="hint">Takes effect the next time the planner starts. The planner and its sessions cannot change this; only you can.</p>

      <h3 className="cap">Planner policy</h3>
      <label className="field">
        <span>Policy</span>
        <select className="in" aria-label="Planner policy" value={own} onChange={(e) => void setPolicyTo(e.target.value)}>
          <option value="inherit">{`Inherit the global policy (${policy?.global ?? "…"})`}</option>
          {POLICIES.map((p) => <option key={p} value={p}>{`${p} — ${POLICY_TEXT[p]}`}</option>)}
        </select>
      </label>
      {own !== "inherit" ? <p className="hint">{POLICY_TEXT[own as PlannerPolicy]}</p> : policy ? <p className="hint">{POLICY_TEXT[policy.global]}</p> : null}
      {policy ? <p className="hint">In force: {policy.policy} ({policy.source})</p> : null}

      <h3 className="cap">Planner model</h3>
      <label className="field">
        <span>Model</span>
        <select className="in" aria-label="Planner model" value={modelInfo?.planner_model ?? ""} onChange={(e) => void setModel(e.target.value)}>
          <option value="">Claude's own default</option>
          <ModelOptions models={modelInfo?.models ?? []} current={modelInfo?.planner_model ?? ""} />
        </select>
        <span className="hint">Typed as /model into the running planner, or used when it next starts.</span>
      </label>

      <h3 className="cap">Autostart</h3>
      <label className="inline">
        <input type="checkbox" aria-label="Start the planner when Hadron opens" checked={autostart} onChange={(e) => void toggleAutostart(e.target.checked)} />
        <span>Start the planner when Hadron opens</span>
      </label>
      <p className="hint">Applies to every workspace.</p>

      <h3 className="cap">Conversation</h3>
      <p className="hint">The planner is {running ? "Running" : "Not running"}.</p>
      <div className="inline">
        <button className="btn" onClick={() => setAdoptOpen(true)}>Adopt planner conversation</button>
        <button className="btn" disabled={running} onClick={() => void life(() => api.planner_start({ workspace_id: wsId }), "Planner started.")}>Start</button>
        <button className="btn" disabled={!running} onClick={() => void life(() => api.planner_restart({ workspace_id: wsId }), "Planner restarted.")}>Restart</button>
        <button className="btn" disabled={!running} onClick={() => void life(() => api.planner_stop({ workspace_id: wsId }), "Planner stopped.")}>Stop</button>
      </div>
      {adoptOpen ? <AdoptPlanner board={board} onClose={() => setAdoptOpen(false)} /> : null}
    </div>
  );
}
