import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { TextareaHTMLAttributes } from "react";
import type { Attachment } from "../api";
import { ACCEPT, filesOf, saveFiles, withAttachments } from "../attach";
import { Icon } from "./ui";

/** The tallest the composer grows before it scrolls, in pixels. */
export const COMPOSER_MAX = 140;

/**
 * A textarea that grows with its text up to `max` pixels. It shows no scrollbar until it reaches the
 * maximum, and never a horizontal one: long words wrap.
 */
export function AutoText({ max = COMPOSER_MAX, className = "", value, ...rest }: { max?: number; value: string } & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [capped, setCapped] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    const full = el.scrollHeight + 2;
    el.style.height = Math.min(full, max) + "px";
    setCapped(full > max);
  }, [value, max]);
  return <textarea ref={ref} rows={1} className={`in ${className}${capped ? " grow-max" : ""}`} value={value} {...rest} />;
}

/** True when this Enter belongs to an input method composing text (Japanese, Chinese, Korean, accents). */
export const composing = (e: { nativeEvent: KeyboardEvent }) => e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229;

interface Props {
  id: string;
  /** The small caption above the field. */
  label: string;
  placeholder: string;
  /** Sends the trimmed text. A rejection keeps the text in the field. */
  onSend: (text: string) => void | Promise<unknown>;
  suggestions?: string[];
  /** Where an attachment that could not be kept is reported. */
  onError?: (text: string) => void;
}

/**
 * Enter sends, Shift+Enter adds a line. Enter while an input method is composing does neither.
 * Images and PDFs pasted, dropped or picked with the clip button are kept under
 * ~/.hadron/attachments and shown as chips; the message then ends with an "Attached: <path>" line
 * per file, so the planner and the session it relays to can read them.
 */
export function Composer({ id, label, placeholder, onSend, suggestions, onError }: Props) {
  const [v, setV] = useState("");
  const [files, setFiles] = useState<Attachment[]>([]);
  const [busy, setBusy] = useState(0);
  const [over, setOver] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const sending = useRef(false);
  const mounted = useRef(true);
  useEffect(() => () => void (mounted.current = false), []);
  const add = async (list: File[]) => {
    if (!list.length) return;
    setBusy((n) => n + 1);
    try {
      const { saved, errors } = await saveFiles(list);
      if (!mounted.current) return;
      if (saved.length) setFiles((cur) => [...cur, ...saved]);
      if (errors.length) onError?.(errors.join(" "));
    } finally {
      if (mounted.current) setBusy((n) => n - 1);
    }
  };
  const send = async () => {
    const text = withAttachments(v.trim(), files.map((f) => f.path));
    if (!text || sending.current || busy) return;
    sending.current = true;
    try {
      await onSend(text);
      if (mounted.current) {
        setV("");
        setFiles([]);
      }
    } catch {
      // The sender reports its own error; the text stays so nothing typed is lost.
    } finally {
      sending.current = false;
    }
  };
  const canSend = (!!v.trim() || files.length > 0) && !busy;
  return (
    <div
      className={"composer" + (over ? " drop" : "")}
      onDragOver={(e) => {
        if (!e.dataTransfer?.types?.includes("Files")) return;
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        if (!e.dataTransfer?.types?.includes("Files")) return;
        e.preventDefault();
        setOver(false);
        void add(filesOf(e.dataTransfer));
      }}
    >
      {suggestions?.length ? (
        <div className="sugg">
          {suggestions.map((s) => <button key={s} type="button" onClick={() => setV(s)}>{s}</button>)}
        </div>
      ) : null}
      <label className="cap" htmlFor={id}>{label}</label>
      {files.length || busy ? (
        <div className="att-row" aria-label="Attachments to send">
          {files.map((f) => (
            <span key={f.path} className="att-chip" title={f.path}>
              {f.url ? <img src={f.url} alt="" /> : <span className="pdf">PDF</span>}
              <span className="nm">{f.name || f.path.split("/").pop()}</span>
              <button type="button" aria-label={`Remove ${f.name || "attachment"}`} onClick={() => setFiles((cur) => cur.filter((x) => x.path !== f.path))}>×</button>
            </span>
          ))}
          {busy ? <span className="hint" role="status">Attaching…</span> : null}
        </div>
      ) : null}
      <div className="row">
        <button className="ibtn" type="button" aria-label="Attach an image or PDF" title="Attach an image or PDF (or paste or drop one)" onClick={() => picker.current?.click()}>
          <Icon name="clip" size={15} />
        </button>
        <input
          ref={picker}
          type="file"
          multiple
          accept={ACCEPT}
          hidden
          aria-label="Choose files to attach"
          onChange={(e) => {
            const list = Array.from(e.target.files ?? []);
            e.target.value = "";
            void add(list);
          }}
        />
        <AutoText
          id={id}
          value={v}
          placeholder={placeholder}
          onChange={(e) => setV(e.target.value)}
          onPaste={(e) => {
            const list = filesOf(e.clipboardData);
            if (!list.length) return;
            e.preventDefault();
            void add(list);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !composing(e)) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <button className="btn pri" type="button" disabled={!canSend} onClick={() => void send()}>Send</button>
      </div>
    </div>
  );
}
