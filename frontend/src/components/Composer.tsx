import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { TextareaHTMLAttributes } from "react";

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
}

/** Enter sends, Shift+Enter adds a line. Enter while an input method is composing does neither. */
export function Composer({ id, label, placeholder, onSend, suggestions }: Props) {
  const [v, setV] = useState("");
  const sending = useRef(false);
  const mounted = useRef(true);
  useEffect(() => () => void (mounted.current = false), []);
  const send = async () => {
    const text = v.trim();
    if (!text || sending.current) return;
    sending.current = true;
    try {
      await onSend(text);
      if (mounted.current) setV("");
    } catch {
      // The sender reports its own error; the text stays so nothing typed is lost.
    } finally {
      sending.current = false;
    }
  };
  return (
    <div className="composer">
      {suggestions?.length ? (
        <div className="sugg">
          {suggestions.map((s) => <button key={s} type="button" onClick={() => setV(s)}>{s}</button>)}
        </div>
      ) : null}
      <label className="cap" htmlFor={id}>{label}</label>
      <div className="row">
        <AutoText
          id={id}
          value={v}
          placeholder={placeholder}
          onChange={(e) => setV(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !composing(e)) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <button className="btn pri" type="button" disabled={!v.trim()} onClick={() => void send()}>Send</button>
      </div>
    </div>
  );
}
