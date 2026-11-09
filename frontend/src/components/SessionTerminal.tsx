import type { SessionStatus } from "../api";
import { Terminal } from "./Terminal";

interface Props {
  label: string;
  status: SessionStatus | null;
  /** What the footer says after the command, such as the nickname and the folder. */
  foot: string;
  /** Shown above the terminal; the card terminal warns that typing goes straight to the session. */
  banner?: React.ReactNode;
  onStart: () => void;
  onRestart: () => void;
  onShiftEscape?: () => void;
}

/** A session's terminal with its state: not started, never started, ended, or running. */
export function SessionTerminal({ label, status, foot, banner, onStart, onRestart, onShiftEscape }: Props) {
  if (!status) return <div className="placeholder">Loading the {label} terminal…</div>;
  const row = status.session;
  if (!row)
    return (
      <div className="termwrap">
        <div className="tnone">
          <p>The {label} has no session yet.</p>
          <button className="btn pri" onClick={onStart}>Start session</button>
        </div>
      </div>
    );
  return (
    <div className="termwrap">
      {banner}
      {status.error ? (
        <div className="tbanner bad" role="alert">
          <span style={{ flex: 1 }}>Could not start the session: {status.error}</span>
          <button className="btn sm" onClick={onRestart}>Try again</button>
        </div>
      ) : !status.running ? (
        <div className="tbanner" role="status">
          <span style={{ flex: 1 }}>The session has ended{row.status === "exited" ? ` (exit code ${row.exit_code})` : ""}.</span>
          <button className="btn sm" onClick={onRestart}>Restart session</button>
        </div>
      ) : null}
      <Terminal sessionId={row.id} label={label} onShiftEscape={onShiftEscape} />
      <div className="tfoot">{foot}</div>
    </div>
  );
}
