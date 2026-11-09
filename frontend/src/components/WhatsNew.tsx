// The "What's new in Hadron" dialog: the running release's notes, plus a list of earlier releases.
import { Fragment, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { ChangelogInfo } from "../api";
import { ModalHead, Scrim } from "./Modals";

/** Plain text with `code` spans; nothing else is interpreted. */
export function renderInline(text: string) {
  return text.split(/(`[^`]+`)/g).map((part, i) =>
    part.length > 2 && part.startsWith("`") && part.endsWith("`") ? <code key={i}>{part.slice(1, -1)}</code> : <Fragment key={i}>{part}</Fragment>,
  );
}

export function WhatsNew({ onClose }: { onClose: () => void }) {
  const [info, setInfo] = useState<ChangelogInfo | null>(null);
  const [shown, setShown] = useState<ChangelogInfo | null>(null);
  const [err, setErr] = useState("");
  const running = useRef<string>("");

  useEffect(() => {
    let live = true;
    api.changelog_get().then((i) => {
      if (!live) return;
      running.current = i.running;
      setInfo(i);
      setShown(i);
    }).catch((e) => live && setErr(e instanceof Error ? e.message : String(e)));
    return () => { live = false; };
  }, []);

  const close = () => {
    if (running.current) void Promise.resolve(api.settings_update({ values: { "changelog.seen": running.current } })).catch(() => undefined);
    onClose();
  };

  const pick = (version: string) => {
    api.changelog_get({ version }).then(setShown).catch((e) => setErr(e instanceof Error ? e.message : String(e)));
  };

  const viewingRunning = !!info && !!shown && shown === info;
  const rel = shown?.release;
  return (
    <Scrim onClose={close}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="wn-t">
        <ModalHead id="wn-t" title="What's new in Hadron" onClose={close} />
        <div className="mb">
          {err ? <p className="hint" role="alert">{err}</p> : null}
          {!shown && !err ? <p className="hint">Loading…</p> : null}
          {shown && !viewingRunning ? <p><a href="#" onClick={(e) => { e.preventDefault(); setShown(info); }}>Back to this version</a></p> : null}
          {shown && !shown.found ? <p>There are no release notes for this version.</p> : null}
          {shown && shown.found && rel ? (
            <>
              <h3>{`Hadron ${rel.version}`}{rel.date ? <span className="hint"> · {rel.date}</span> : null}</h3>
              {rel.sections.map((s) => (
                <Fragment key={s.title}>
                  <h4>{s.title}</h4>
                  <ul>{s.items.map((it, i) => <li key={i}>{renderInline(it)}</li>)}</ul>
                </Fragment>
              ))}
            </>
          ) : null}
          {info && info.releases.length > 0 ? (
            <details>
              <summary>Earlier releases</summary>
              <ul>
                {info.releases.map((r) => (
                  <li key={r.version}>
                    <button className="btn sm quiet" type="button" onClick={() => pick(r.version)}>{r.version}</button>
                    {r.date ? <span className="hint"> {r.date}</span> : null} {r.summary ? <span>{r.summary}</span> : null}
                  </li>
                ))}
              </ul>
            </details>
          ) : null}
        </div>
        <div className="inline" style={{ padding: "0 16px 16px", justifyContent: "flex-end" }}>
          <button className="btn pri" type="button" onClick={close}>Got it</button>
        </div>
      </div>
    </Scrim>
  );
}
