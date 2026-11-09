import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import * as api from "../api";
import type { PromptInfo, PromptKind } from "../api";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** A textarea that grows with its text, up to max pixels. */
export function AutoText({ value, onChange, max = 520, id, label }: { value: string; onChange: (v: string) => void; max?: number; id?: string; label: string }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, max) + "px";
  }, [value, max]);
  return (
    <textarea
      ref={ref}
      id={id}
      className="in mono"
      style={{ fontSize: 12, lineHeight: 1.55, resize: "vertical", minHeight: 120 }}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      aria-label={label}
    />
  );
}

/** Settings → Prompts: the Planner / Sessions switch, variable chips, the template editor, Save, Reset to default and the live preview. */
export function PromptsTab({ board }: { board: Board }) {
  const { ws, selected, actions } = board;
  const wsId = ws?.id ?? "";
  const [kind, setKind] = useState<PromptKind>("planner");
  const [info, setInfo] = useState<PromptInfo | null>(null);
  const [draft, setDraft] = useState("");
  const [preview, setPreview] = useState("");
  const [unknown, setUnknown] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!wsId) return;
    try {
      const p = await api.prompt_get({ workspace_id: wsId, kind });
      setInfo(p);
      setDraft(p.template);
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [wsId, kind, actions]);
  useEffect(() => {
    setInfo(null);
    void load();
  }, [load]);

  // The preview renders the unsaved draft on the backend, so it shows what a session would really get.
  const sampleCard = kind === "session" ? selected?.nick : undefined;
  useEffect(() => {
    if (!wsId || !info) return;
    const t = setTimeout(() => {
      api
        .prompt_render({ workspace_id: wsId, kind, template: draft, ...(sampleCard ? { card: sampleCard } : {}) })
        .then((r) => {
          setPreview(r.text);
          setUnknown(r.unknown);
        })
        .catch((e) => setPreview(errText(e)));
    }, 250);
    return () => clearTimeout(t);
  }, [wsId, kind, draft, sampleCard, info]);

  const save = async () => {
    setBusy(true);
    try {
      const p = await api.prompt_set({ workspace_id: wsId, kind, template: draft });
      setInfo(p);
      actions.toast(p.warning ? `Saved. ${p.warning}` : "Prompt saved.");
      await actions.refetch();
    } catch (e) {
      actions.toast(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const reset = async () => {
    setBusy(true);
    try {
      const p = await api.prompt_reset({ workspace_id: wsId, kind });
      setInfo(p);
      setDraft(p.template);
      actions.toast("Back to Hadron's default prompt.");
    } catch (e) {
      actions.toast(errText(e));
    } finally {
      setBusy(false);
    }
  };

  const custom = info?.custom ?? false;
  const dirty = info ? draft !== info.template : false;
  return (
    <>
      <div className="inline">
        <h4 style={{ flex: 1 }}>Prompts</h4>
        <div className="seg" role="tablist" aria-label="Which prompt">
          <button className={kind === "planner" ? "on" : ""} role="tab" aria-selected={kind === "planner"} onClick={() => setKind("planner")}>
            Planner
          </button>
          <button className={kind === "session" ? "on" : ""} role="tab" aria-selected={kind === "session"} onClick={() => setKind("session")}>
            Sessions
          </button>
        </div>
      </div>
      <p className="sub">
        {kind === "planner"
          ? "The system prompt the planner starts with. It tells the planner it runs inside Hadron, how to route chat, and what it may do on its own."
          : "The system prompt every card session starts with. It tells the session it runs inside Hadron, that the planner is its only channel to you, and how to report."}{" "}
        Hadron fills in the {"{{variables}}"} when the session starts. The planner can change this too, with prompt_set.
      </p>
      <div className="inline">
        <span className={"pill " + (custom ? "acc" : "")}>{custom ? `custom for ${ws?.name}` : "Hadron default"}</span>
        <span className="hint">Variables:</span>
        {(info?.variables ?? []).map((v) => (
          <button key={v} className="pill mono" style={{ fontSize: 11 }} onClick={() => setDraft((d) => d + `{{${v}}}`)} aria-label={`Insert ${v}`}>
            {`{{${v}}}`}
          </button>
        ))}
      </div>
      <label className="field">
        <span>Template</span>
        <AutoText id={"prompt-" + kind} value={draft} onChange={setDraft} label="Template" />
      </label>
      {unknown.length ? (
        <div className="hint" role="status" style={{ color: "var(--warn)" }}>
          Unknown variable(s) {unknown.map((u) => `{{${u}}}`).join(", ")} will be left as written.
        </div>
      ) : null}
      <div className="inline">
        <button className="btn pri" disabled={!dirty || busy || !draft.trim()} onClick={() => void save()}>
          Save
        </button>
        <button className="btn" disabled={!custom || busy} onClick={() => void reset()}>
          Reset to default
        </button>
        <span className="hint">{kind === "planner" ? "Takes effect when the planner restarts." : "Takes effect for sessions started after you save."}</span>
      </div>
      <div className="cap">What {kind === "planner" ? "the planner" : (sampleCard ?? "a new session")} receives</div>
      <div className="code" style={{ whiteSpace: "pre-wrap" }} aria-label="Rendered prompt">
        {preview}
      </div>
    </>
  );
}
