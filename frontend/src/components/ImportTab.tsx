import { useState } from "react";
import * as api from "../api";
import type { ImportPlan } from "../api";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Settings → Import: bring an older planner's run folder in as cards. A preview changes nothing. */
export function ImportTab({ board }: { board: Board }) {
  const { ws, actions } = board;
  const [dir, setDir] = useState(`${ws?.root ?? ""}/.ai/runs/planner`);
  const [plan, setPlan] = useState<ImportPlan | null>(null);
  const [previewed, setPreviewed] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const fail = (e: unknown) => {
    const m = errText(e);
    setError(m);
    actions.toast(m);
  };
  const preview = async () => {
    if (!ws) return;
    setBusy(true);
    setError("");
    try {
      const p = await api.workspace_import_planner_dir({ workspace_id: ws.id, dir: dir.trim(), dry_run: true });
      setPlan(p);
      setPreviewed(dir.trim());
    } catch (e) {
      setPlan(null);
      setPreviewed("");
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const doImport = async () => {
    if (!ws) return;
    setBusy(true);
    setError("");
    try {
      const p = await api.workspace_import_planner_dir({ workspace_id: ws.id, dir: previewed, dry_run: false });
      setPlan(null);
      setPreviewed("");
      actions.toast(`Imported ${p.created} cards. Open one and press Resume to continue its conversation.`);
      await actions.refetch();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const canImport = !busy && !!plan && plan.created > 0 && previewed === dir.trim();

  return (
    <>
      <h4 style={{ margin: 0 }}>Import</h4>
      <p className="sub">Bring in an older planner's run folder: its sessions become cards you can resume, with their reports, questions and gates. Nothing is started, and importing twice changes nothing.</p>
      <div className="fg">
        <label htmlFor="import-dir">Folder</label>
        <div>
          <input id="import-dir" className="in mono" style={{ width: "100%" }} value={dir} onChange={(e) => setDir(e.target.value)} />
          <div className="hint">Paste the folder path.</div>
        </div>
      </div>
      <div className="rrow">
        <button className="btn sm" disabled={busy || !dir.trim()} onClick={() => void preview()}>Preview</button>
        <button className="btn sm pri" disabled={!canImport} onClick={() => void doImport()}>Import</button>
      </div>
      {error ? <div className="tbanner bad" role="alert">{error}</div> : null}
      {!plan ? (
        error ? null : <div className="empty">Choose a folder and press Preview.</div>
      ) : (
        <>
          <p role="status">
            {`${plan.created} cards to create, ${plan.skipped} skipped · ${plan.messages} messages, ${plan.questions} questions, ${plan.gates} gates`}
            {plan.unmatched_log_lines > 0 ? ` · ${plan.unmatched_log_lines} log lines matched no card` : ""}
          </p>
          {plan.warnings?.length ? <ul>{plan.warnings.map((w, i) => <li key={i} className="hint">{w}</li>)}</ul> : null}
          {plan.created === 0 ? <div className="empty">Nothing new to import.</div> : null}
          {plan.cards?.length ? (
            <table aria-label="Import preview" style={{ width: "100%", fontSize: 12.5 }}>
              <thead>
                <tr><th align="left">Nickname</th><th align="left">Key</th><th align="left">Column</th><th align="right">Messages</th><th align="right">Questions</th><th align="right">Gates</th><th align="left">Action</th></tr>
              </thead>
              <tbody>
                {plan.cards.map((c) => (
                  <tr key={c.nick}>
                    <td>{c.nick}</td>
                    <td className="mono">{c.key}</td>
                    <td>{c.column}{c.stage ? <> <span className="pill">{c.stage}</span></> : null}</td>
                    <td align="right">{c.messages}</td>
                    <td align="right">{c.questions}{c.open_questions > 0 ? <> <span className="pill warn">{`${c.open_questions} open`}</span></> : null}</td>
                    <td align="right">{c.gates}</td>
                    <td title={c.reason}>{c.action === "skip" ? `skip${c.reason ? `: ${c.reason}` : ""}` : "create"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
        </>
      )}
    </>
  );
}
