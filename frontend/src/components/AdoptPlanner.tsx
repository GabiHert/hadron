import { useEffect, useState } from "react";
import * as api from "../api";
import type { Adoptable } from "../api";
import type { Board } from "../state/useBoard";
import { AdoptList } from "./AdoptList";
import { ModalHead, Scrim } from "./Modals";
import { ProfileGrant, ProfileOptions, useProfiles } from "./ProfileGrant";

/** Adopt an existing Claude Code conversation as the workspace's planner. */
export function AdoptPlanner({ board, onClose }: { board: Board; onClose: () => void }) {
  const { ws, actions } = board;
  const wsId = ws?.id ?? "";
  const [picked, setPicked] = useState<Adoptable | null>(null);
  const [busy, setBusy] = useState(false);
  const [profile, setProfile] = useState("");
  const profiles = useProfiles(wsId, actions.toast);
  useEffect(() => {
    if (!wsId) return;
    let alive = true;
    api
      .planner_get({ workspace_id: wsId })
      .then((s) => alive && setProfile(s.launch_profile ?? ""))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [wsId]);
  if (!ws) return null;
  const adopt = async () => {
    if (!picked) return;
    setBusy(true);
    const out = await actions.run(() =>
      api.planner_adopt({ workspace_id: ws.id, claude_session_id: picked.claude_session_id, ...(profile ? { launch_profile: profile } : {}), ...(picked.cwd ? { cwd: picked.cwd } : {}) }),
    );
    setBusy(false);
    if (!out) return;
    actions.toast(out.warning);
    onClose();
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="ap-t">
        <ModalHead id="ap-t" title="Adopt planner conversation" onClose={onClose} />
        <div className="mb">
          <AdoptList workspaceId={ws.id} root={ws.root} picked={picked?.claude_session_id ?? ""} onPick={setPicked} onError={actions.toast} />
          <label className="field">
            <span>Planner launch profile</span>
            <select className="in" aria-label="Adopted planner launch profile" value={profile} onChange={(e) => setProfile(e.target.value)}>
              <ProfileOptions profiles={profiles} current={profile} />
            </select>
          </label>
          <ProfileGrant profile={profiles.find((p) => p.name === profile)} />
        </div>
        <div className="mf">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn pri" disabled={!picked || busy} onClick={() => void adopt()}>Adopt as planner</button>
        </div>
      </div>
    </Scrim>
  );
}
