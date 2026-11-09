import { useEffect, useState } from "react";
import * as api from "../api";
import type { LaunchProfile } from "../api";

/** A workspace's launch profiles, loaded once per workspace. */
export function useProfiles(wsId: string, onError: (t: string) => void): LaunchProfile[] {
  const [profiles, setProfiles] = useState<LaunchProfile[]>([]);
  useEffect(() => {
    let alive = true;
    api
      .launch_profile_list(wsId ? { workspace_id: wsId } : {})
      .then((list) => alive && setProfiles(list))
      .catch((e) => onError(e instanceof Error ? e.message : String(e)));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId]);
  return profiles;
}

/** Options for a launch profile select that may also mean "none". */
export function ProfileOptions({ profiles, current }: { profiles: LaunchProfile[]; current: string }) {
  return (
    <>
      <option value="">None (plain claude)</option>
      {profiles.map((p) => <option key={p.id} value={p.name}>{p.name}</option>)}
      {current && !profiles.some((p) => p.name === current) ? <option value={current}>{current}</option> : null}
    </>
  );
}

/** Exactly what a profile grants: the command line, its model, and env variable names (never values). */
export function ProfileGrant({ profile }: { profile: LaunchProfile | undefined }) {
  const envNames = profile ? Object.keys(profile.env ?? {}).sort() : [];
  return (
    <div className="code" aria-label="What the planner will run">
      {profile ? (
        <>
          <div className="mono">{[profile.command || "claude", ...(profile.args ?? [])].join(" ")}</div>
          {profile.model ? <div>Model: <span className="mono">{profile.model}</span></div> : null}
          {envNames.length ? <div>Environment variables: <span className="mono">{envNames.join(", ")}</span></div> : null}
        </>
      ) : (
        <div className="mono">claude</div>
      )}
    </div>
  );
}
