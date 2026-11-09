import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { MutableRefObject, TextareaHTMLAttributes } from "react";
import type { Attachment, Card, Column, ComposerTarget } from "../api";
import { ACCEPT, filesOf, saveFiles, withAttachments } from "../attach";
import { isCommand, unescapeSlash } from "../complete";
import { useSuggest } from "./Suggest";
import { Icon } from "./ui";

/** The tallest the composer grows before it scrolls, in pixels. */
export const COMPOSER_MAX = 140;

/**
 * A textarea that grows with its text up to `max` pixels. It shows no scrollbar until it reaches the
 * maximum, and never a horizontal one: long words wrap.
 */
export function AutoText({ max = COMPOSER_MAX, className = "", value, textRef, ...rest }: { max?: number; value: string; textRef?: MutableRefObject<HTMLTextAreaElement | null> } & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const [capped, setCapped] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    const full = el.scrollHeight + 2;
    el.style.height = Math.min(full, max) + "px";
    setCapped(full > max);
  }, [value, max]);
  return (
    <textarea
      ref={(el) => {
        ref.current = el;
        if (textRef) textRef.current = el;
      }}
      rows={1}
      className={`in ${className}${capped ? " grow-max" : ""}`}
      value={value}
      {...rest}
    />
  );
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
  /** The session the / and @ menus complete against (its commands and its folder). Without it there is no menu. */
  target?: ComposerTarget;
  /** Cards the @ menu offers, before the files. */
  cards?: Card[];
  columns?: Column[];
  /** Offer @planner in the @ menu (not in the planner's own chat). */
  mentionPlanner?: boolean;
  /**
   * Sends a message that starts with one / straight to the session's terminal. Without it, a / is
   * plain text. With it, a leading // sends a literal slash message to the planner instead.
   */
  onCommand?: (text: string) => Promise<unknown>;
  /** false when the session is not running: a /command then shows why and a Start button. */
  running?: boolean;
  /** Starts the session, from the not-running error. */
  onStart?: () => void | Promise<unknown>;
  /** Who runs the command, for the error: "billing-fix's session" or "The planner". */
  sessionName?: string;
}

/**
 * Enter sends, Shift+Enter adds a line. Enter while an input method is composing does neither.
 * Images and PDFs pasted, dropped or picked with the clip button are kept under
 * ~/.hadron/attachments and shown as chips; the message then ends with an "Attached: <path>" line
 * per file, so the planner and the session it relays to can read them.
 *
 * With a target, "/" at the start opens the session's slash commands and "@" after a space its
 * cards and files. With onCommand, a message that starts with "/" goes to the session's terminal,
 * not the planner; "//" sends a literal slash to the planner.
 */
export function Composer({ id, label, placeholder, onSend, suggestions, onError, target, cards, columns, mentionPlanner = false, onCommand, running, onStart, sessionName = "The session" }: Props) {
  const [v, setV] = useState("");
  const [files, setFiles] = useState<Attachment[]>([]);
  const [busy, setBusy] = useState(0);
  const [over, setOver] = useState(false);
  const [caret, setCaret] = useState(0);
  const [focused, setFocused] = useState(false);
  const [imeOn, setImeOn] = useState(false);
  const [cmdErr, setCmdErr] = useState<{ text: string; start: boolean } | null>(null);
  const [starting, setStarting] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const field = useRef<HTMLTextAreaElement | null>(null);
  const pendingCaret = useRef<number | null>(null);
  const sending = useRef(false);
  const mounted = useRef(true);
  useEffect(() => () => void (mounted.current = false), []);
  useLayoutEffect(() => {
    const c = pendingCaret.current;
    if (c === null || !field.current) return;
    pendingCaret.current = null;
    field.current.setSelectionRange(c, c);
  }, [v]);

  const suggest = useSuggest({
    id,
    value: v,
    caret,
    active: focused && !imeOn,
    target,
    commands: !!onCommand,
    cards,
    columns,
    planner: mentionPlanner,
    onPick: (nv, nc) => {
      pendingCaret.current = nc;
      setV(nv);
      setCaret(nc);
      setCmdErr(null);
    },
  });

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
  const clear = () => {
    if (!mounted.current) return;
    setV("");
    setCaret(0);
    setFiles([]);
    setCmdErr(null);
  };
  const raw = v.trim();
  const toTerminal = !!onCommand && isCommand(raw);
  const send = async () => {
    if (sending.current || busy) return;
    if (toTerminal && onCommand) {
      const name = raw.split(/\s/)[0];
      if (files.length) {
        setCmdErr({ text: `${name} goes to the terminal as typed, without attachments. Remove them, or send them in another message.`, start: false });
        return;
      }
      if (running === false) {
        setCmdErr({ text: `${sessionName} is not running, so ${name} was not sent. Start it, then send again.`, start: !!onStart });
        return;
      }
      sending.current = true;
      try {
        await onCommand(raw);
        clear();
      } catch (e) {
        const why = e instanceof Error ? e.message : String(e);
        const stopped = /no session yet|has ended|never started|not running/i.test(why);
        if (mounted.current) setCmdErr({ text: stopped ? `${sessionName} is not running, so ${name} was not sent. Start it, then send again.` : `${name} was not sent: ${why}`, start: stopped && !!onStart });
      } finally {
        sending.current = false;
      }
      return;
    }
    const text = withAttachments(onCommand ? unescapeSlash(raw) : raw, files.map((f) => f.path));
    if (!text) return;
    sending.current = true;
    try {
      await onSend(text);
      clear();
    } catch {
      // The sender reports its own error; the text stays so nothing typed is lost.
    } finally {
      sending.current = false;
    }
  };
  const canSend = (!!raw || files.length > 0) && !busy;
  const syncCaret = (el: HTMLTextAreaElement) => setCaret(el.selectionStart ?? el.value.length);
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
      {suggest.menu}
      {suggestions?.length ? (
        <div className="sugg">
          {suggestions.map((s) => <button key={s} type="button" onClick={() => setV(s)}>{s}</button>)}
        </div>
      ) : null}
      <div className="cap-row">
        <label className="cap" htmlFor={id}>{label}</label>
        {toTerminal ? <span className="to-term" title="A message that starts with / is typed into the session's terminal. Start it with // to send a literal slash to the planner.">→ terminal</span> : null}
      </div>
      {cmdErr ? (
        <div className="cmd-err" role="alert">
          <span>{cmdErr.text}</span>
          {cmdErr.start && onStart ? (
            <button
              type="button"
              className="btn sm"
              disabled={starting}
              onClick={async () => {
                setStarting(true);
                try {
                  await onStart();
                  if (mounted.current) setCmdErr(null);
                } finally {
                  if (mounted.current) setStarting(false);
                }
              }}
            >
              Start
            </button>
          ) : null}
        </div>
      ) : null}
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
          textRef={field}
          value={v}
          placeholder={placeholder}
          {...(target ? suggest.ariaProps : {})}
          onChange={(e) => {
            setV(e.target.value);
            syncCaret(e.target);
            if (cmdErr) setCmdErr(null);
          }}
          onSelect={(e) => syncCaret(e.currentTarget)}
          onFocus={(e) => {
            setFocused(true);
            syncCaret(e.currentTarget);
          }}
          onBlur={() => setFocused(false)}
          onCompositionStart={() => setImeOn(true)}
          onCompositionEnd={(e) => {
            setImeOn(false);
            syncCaret(e.currentTarget);
          }}
          onPaste={(e) => {
            const list = filesOf(e.clipboardData);
            if (!list.length) return;
            e.preventDefault();
            void add(list);
          }}
          onKeyDown={(e) => {
            if (composing(e)) return;
            if (suggest.onKeyDown(e)) return;
            if (e.key === "Enter" && !e.shiftKey) {
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
