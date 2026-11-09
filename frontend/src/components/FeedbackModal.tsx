import { useEffect, useState } from "react";
import * as api from "../api";
import type { FeedbackKind } from "../api";
import type { Board } from "../state/useBoard";
import { AutoText } from "./Composer";
import { ModalHead, Scrim } from "./Modals";

export const KINDS: [FeedbackKind, string][] = [["bug", "Bug"], ["idea", "Idea"], ["annoyance", "Annoyance"], ["praise", "Praise"]];

/** The header's speech bubble: one kind, one note, optional diagnostics. */
export function FeedbackModal({ board, area, onClose }: { board: Board; area: string; onClose: () => void }) {
  const [kind, setKind] = useState<FeedbackKind>("idea");
  const [text, setText] = useState("");
  const [attach, setAttach] = useState(false);
  const [busy, setBusy] = useState(false);
  const touched = useState({ v: false })[0];

  useEffect(() => {
    let alive = true;
    api.settings_get({ key: "debug.logging" }).then((s) => { if (alive && !touched.v) setAttach(s["debug.logging"] === true); }).catch(() => undefined);
    return () => { alive = false; };
  }, [touched]);

  const send = async () => {
    if (!text.trim() || busy) return;
    setBusy(true);
    try {
      await api.feedback_create({ kind, text: text.trim(), area, attach_diagnostics: attach });
      board.actions.toast("Thanks. It's in Settings → Feedback.");
      onClose();
    } catch (e) {
      board.actions.toast(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  };

  return (
    <Scrim onClose={onClose}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="fb-t">
        <ModalHead id="fb-t" title="Send feedback" onClose={onClose} />
        <div role="radiogroup" aria-label="Kind" className="rrow" style={{ gap: 6 }}>
          {KINDS.map(([k, l]) => (
            <button key={k} type="button" className={"btn" + (kind === k ? " on" : "")} aria-pressed={kind === k} onClick={() => setKind(k)}>{l}</button>
          ))}
        </div>
        <label className="hint" htmlFor="fb-text" style={{ display: "block", marginTop: 10 }}>What do you want to tell us?</label>
        <AutoText id="fb-text" max={240} value={text} onChange={(e) => setText(e.target.value)} required style={{ width: "100%" }} />
        <label className="rrow" style={{ gap: 8, marginTop: 10 }}>
          <input type="checkbox" checked={attach} onChange={(e) => { touched.v = true; setAttach(e.target.checked); }} />
          <span>Attach diagnostics (version, macOS, settings without secrets, recent log lines)</span>
        </label>
        <div className="rrow" style={{ justifyContent: "flex-end", gap: 8, marginTop: 12 }}>
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn pri" disabled={!text.trim() || busy} onClick={() => void send()}>Send</button>
        </div>
      </div>
    </Scrim>
  );
}
