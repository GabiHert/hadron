import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import type { Message, Question } from "../api";
import { hm } from "../lib";
import { AutoText } from "./Composer";
import { Widget } from "./Widget";
import { Markdown } from "./Markdown";
import { usePinnedIds, useWidgetScripts } from "../state/useDash";
import * as api from "../api";

/** How long "Planner is replying…" may show for one message. */
export const TYPING_MAX_MS = 2 * 60 * 1000;

/** A message's text as markdown, with thumbnails of the files it attaches. */
export function Md({ text }: { text: string }) {
  return <Markdown text={text} attachments />;
}

/** Why a message did not reach the planner, in words the user can act on. */
export function reasonText(reason: string): string {
  if (reason === "planner_not_running") return "Not delivered: the planner is not running. Start it from the planner's Terminal view, then send again.";
  if (reason.startsWith("type_failed: ")) return `Not delivered: Hadron could not type into the planner (${reason.slice("type_failed: ".length)}).`;
  return `Not delivered: ${reason || "the planner did not get it"}.`;
}

const REPORT_PILL: Record<string, string> = { question: "warn", blocked: "bad", done: "acc", progress: "ok" };

/** One message line. A question's report is drawn by the thread as a question card instead. */
export function ChatLine({ m, onPin, pinnedIds, scripts = false }: { m: Message; onPin?: (id: string) => void; pinnedIds?: Set<string>; scripts?: boolean }) {
  const when = hm(m.created_at);
  if (m.kind === "widget")
    return (
      <>
        <div className="who">{m.author} · {when} · widget</div>
        <Widget title={m.widget_title} html={m.widget_html} scripts={scripts} onPin={onPin ? () => onPin(m.id) : undefined} pinned={pinnedIds?.has(m.id)} />
      </>
    );
  if (m.kind === "event" || m.author === "rule" || m.author === "system") return <div className="b-ev">{m.text}</div>;
  if (m.kind === "report")
    return (
      <>
        <div className="who">session → planner · <span className={`pill ${REPORT_PILL[m.report_kind] ?? ""}`}>{m.report_kind}</span> · {when}</div>
        <div className="b-rep"><Md text={m.text} /></div>
        {!m.delivered ? <div className="undelivered" role="note" title={m.reason}>The planner did not hear this report. {reasonText(m.reason)}</div> : null}
      </>
    );
  if (m.author === "planner")
    return (
      <>
        <div className="who">planner · {when}</div>
        <div className="b-pl"><Md text={m.text} /></div>
      </>
    );
  return (
    <>
      <div className="who r">you · {when}</div>
      <div className="b-me"><Md text={m.text} /></div>
      {!m.delivered ? <div className="undelivered" role="note" title={m.reason}>{reasonText(m.reason)}</div> : null}
    </>
  );
}

const nonEmpty = (s?: string) => !!s && s.trim().length > 0;

interface QCProps {
  q: Question;
  onAnswer: (selections: string[], notes: string) => void | Promise<unknown>;
}

/**
 * A question in the conversation. Open, it shows its options (numbered, one or several), the
 * approval details, a Notes field and the Answer button. Answered or withdrawn, it shrinks to a line.
 */
export function QuestionCard({ q, onAnswer }: QCProps) {
  const [picked, setPicked] = useState<number[]>(q.multi || q.options.length === 0 ? [] : [0]);
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const label = `Q-${String(q.seq).padStart(2, "0")}`;

  if (q.status !== "open") {
    const a = q.answer;
    return (
      <div className="qdone" data-testid={`question-${q.status}`}>
        <span className="cap">{label} · {q.kind}</span>
        <Markdown className="qtext" text={q.text} />
        {q.status === "withdrawn" ? (
          <span className="hint">Withdrawn.</span>
        ) : a ? (
          <span className="hint">
            Answered{a.via && a.via !== "you" && a.via !== "planner-relay" ? ` by the ${a.via}` : ""}: <b style={{ fontWeight: 500 }}>{a.selections.length ? a.selections.join(", ") : "see notes"}</b>
            {a.via === "planner-relay" ? <> · relayed by the planner from your chat message</> : null}
            {nonEmpty(a.notes) ? <> · notes: {a.notes}</> : null}
          </span>
        ) : null}
      </div>
    );
  }

  const toggle = (i: number) =>
    setPicked((cur) => (q.multi ? (cur.includes(i) ? cur.filter((x) => x !== i) : [...cur, i].sort((a, b) => a - b)) : [i]));
  const canAnswer = picked.length > 0 || nonEmpty(notes);
  const answer = async () => {
    if (!canAnswer || busy) return;
    setBusy(true);
    try {
      await onAnswer(picked.map((i) => q.options[i].label), notes.trim());
      setNotes("");
    } catch {
      // the sender reported it; the card stays open with the choice intact
    } finally {
      setBusy(false);
    }
  };
  const ap = q.approval;
  const rev = ap ? (ap.reversible === true || ap.reversible === "yes" ? "yes" : ap.reversible === "partial" ? "partial" : "no") : "no";
  return (
    <div className={"qbox" + (q.kind === "approval" ? " appr" : "")} role="group" aria-label={`Question ${label}`}>
      <span className="cap">{label} · {q.kind}</span>
      <Markdown className="qtext" text={q.text} />
      {q.context ? <Markdown className="hint" text={q.context} attachments /> : null}
      {nonEmpty(q.meanwhile) ? <span className="hint" aria-label="Meanwhile">While you decide: {q.meanwhile}</span> : null}
      {ap ? (
        <>
          <dl className="kv">
            <dt>Target</dt><dd>{ap.target}</dd>
            <dt>Runs as</dt><dd>{ap.runs_as || "not stated"}</dd>
            <dt>Reversible</dt>
            <dd style={rev === "yes" ? undefined : { color: "var(--warn)" }}>{rev === "yes" ? "Yes" : rev === "no" ? "No" : "Partly"}</dd>
          </dl>
          {nonEmpty(ap.reversible_note) ? <span className="hint" aria-label="Reversible note">{ap.reversible_note}</span> : null}
          <div style={{ position: "relative" }}>
            {nonEmpty(ap.command_lang) ? (
              <span aria-label="Command language" className="mono" style={{ position: "absolute", top: 4, right: 8, fontSize: 10, textTransform: "uppercase", color: "var(--fg3)" }}>{ap.command_lang}</span>
            ) : null}
            <div className="code" aria-label="Command" style={{ whiteSpace: "pre-wrap" }}>{ap.command}</div>
          </div>
        </>
      ) : null}
      {q.options.map((o, i) => (
        <button key={o.label} type="button" className={"opt" + (picked.includes(i) ? " on" : "")} aria-pressed={picked.includes(i)} onClick={() => toggle(i)}>
          <span className="kbd">{i + 1}</span>
          <span>
            <b style={{ fontWeight: 500 }}>{o.label}</b>
            {o.description ? <><br /><span className="hint">{o.description}</span></> : null}
          </span>
        </button>
      ))}
      {q.multi ? <span className="hint">You can pick more than one.</span> : null}
      <label className="field">
        <span>Notes <span className="hint" style={{ fontWeight: 400 }}>(optional, sent with your answer)</span></span>
        <AutoText id={`q-notes-${q.id}`} value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Context, conditions, or what to do differently" />
      </label>
      <div className="inline" style={{ justifyContent: "flex-end" }}>
        <span className="hint" style={{ flex: 1 }}>{q.kind === "approval" ? "The planner may also answer this under the planner policy." : ""}</span>
        <button className="btn pri" type="button" disabled={!canAnswer || busy} onClick={() => void answer()}>Answer</button>
      </div>
    </div>
  );
}

/** True while the planner is probably writing: the newest message is yours, was delivered, and is under two minutes old. */
export function plannerIsReplying(messages: Message[], now: number): boolean {
  const last = messages[messages.length - 1];
  if (!last || last.author !== "you" || last.kind !== "text" || !last.delivered) return false;
  const age = now - new Date(last.created_at).getTime();
  return age >= 0 && age < TYPING_MAX_MS;
}

/** plannerIsReplying, re-evaluated when the two minutes run out. */
export function useReplying(messages: Message[]): boolean {
  const [now, setNow] = useState(() => Date.now());
  const last = messages[messages.length - 1];
  useEffect(() => {
    setNow(Date.now());
    if (!last) return;
    const left = new Date(last.created_at).getTime() + TYPING_MAX_MS - Date.now();
    if (left <= 0) return;
    const t = setTimeout(() => setNow(Date.now()), left + 50);
    return () => clearTimeout(t);
  }, [last?.id, last?.created_at]); // eslint-disable-line react-hooks/exhaustive-deps
  return plannerIsReplying(messages, now);
}

interface ThreadProps {
  messages: Message[];
  questions: Question[];
  onAnswer: (questionId: string, selections: string[], notes: string) => void | Promise<unknown>;
  empty: ReactNode;
  /** the scrolling element's class: "chat" in the planner, "cbody" in a card */
  className: string;
  /** a key that changes when the conversation changes (planner, or which card), so the view starts at the bottom */
  scope: string;
  /** the workspace the thread belongs to; with it, widgets can be pinned to its dashboard */
  workspaceId?: string;
  /** where a failed pin is reported */
  onError?: (text: string) => void;
}

/** The scrolling list of lines. A question's report is drawn as its card, in place. */
export function ChatThread({ messages, questions, onAnswer, empty, className, scope, workspaceId = "", onError }: ThreadProps) {
  const hasWidget = messages.some((m) => m.kind === "widget");
  const scripts = useWidgetScripts(hasWidget);
  const pinnedFromDash = usePinnedIds(workspaceId, hasWidget);
  const [pinnedNow, setPinnedNow] = useState<Set<string>>(new Set());
  const pinnedIds = new Set([...pinnedFromDash, ...pinnedNow]);
  const pin = async (id: string) => {
    try {
      await api.widget_pin({ message_id: id });
      setPinnedNow((p) => new Set(p).add(id));
    } catch (e) {
      onError?.(e instanceof Error ? e.message : String(e));
    }
  };
  const box = useRef<HTMLDivElement>(null);
  const replying = useReplying(messages);
  const byId = new Map(questions.map((q) => [q.id, q]));
  const stick = useRef(true);
  useEffect(() => {
    stick.current = true;
  }, [scope]);
  useEffect(() => {
    const el = box.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [messages.length, questions.length, replying, scope]);
  return (
    <div
      className={className}
      ref={box}
      role="log"
      aria-label="Conversation"
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
      }}
    >
      {messages.length === 0 ? empty : null}
      {messages.map((m) => {
        const q = m.question_id ? byId.get(m.question_id) : undefined;
        if (m.kind === "report" && m.report_kind === "question" && q)
          return <QuestionCard key={m.id} q={q} onAnswer={(sel, notes) => onAnswer(q.id, sel, notes)} />;
        return <ChatLine key={m.id} m={m} scripts={scripts} pinnedIds={pinnedIds} onPin={workspaceId ? (id) => void pin(id) : undefined} />;
      })}
      {replying ? <div className="typing" role="status">Planner is replying…</div> : null}
    </div>
  );
}
