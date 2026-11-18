// The tutorial: a short guided tour of Hadron that dims the app and spotlights the real element
// each step explains, moving smoothly from one to the next. Where the element is not on screen (an
// empty board, a collapsed panel, no card open) the step shows an animated mock instead. Nothing
// it shows is real data, and it never creates cards or workspaces. ← → move, Esc skips.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Icon } from "./ui";

export interface TourStep {
  id: string;
  chapter: ChapterId;
  title: string;
  body: string;
  /** a CSS selector of the real element to spotlight; none for a centred step */
  target?: string;
  /** the small animation shown with the caption, and as the mock when the target is missing */
  illus?: Illus;
}

export type ChapterId = "basics" | "planner" | "automate" | "mcp";

/** The tour's chapters, in order. Settings, ⌘K and tutorial_show can start at any of them. */
export const CHAPTERS: { id: ChapterId; title: string }[] = [
  { id: "basics", title: "Basics" },
  { id: "planner", title: "Working with the planner" },
  { id: "automate", title: "Automate" },
  { id: "mcp", title: "MCP" },
];

type Illus =
  | "session" | "tabs" | "board" | "newcard" | "chatterm" | "send" | "question"
  | "widget" | "dash" | "integrations" | "routines" | "mcptools" | "mcpconnect" | "mcplive" | "handoff";

export const TOUR_STEPS: TourStep[] = [
  { id: "welcome", chapter: "basics", title: "Welcome to Hadron", body: "Every card on your board is a Claude Code session. You talk to one planner, and it runs the cards for you.", illus: "session" },
  { id: "workspaces", chapter: "basics", title: "Workspaces", body: "Each tab is a workspace: one project with its own board, planner and sessions. Switch with the tabs or ⌘1 to ⌘9.", target: '[data-tour="workspaces"]', illus: "tabs" },
  { id: "board", chapter: "basics", title: "The board", body: "Cards move across the columns as their sessions work. Needs you is where a card waits for you: a question or an approval.", target: '[data-tour="needs-you"]', illus: "board" },
  { id: "newcard", chapter: "basics", title: "Make a card", body: "New session makes a card: give it a name and a task, and Hadron starts Claude Code for it. You can also just ask the planner.", target: '[data-tour="new-card"]', illus: "newcard" },
  { id: "chatterm", chapter: "basics", title: "Chat or Terminal", body: "Open a card to see its Chat: reports, questions and what you said. Terminal is the live session, when you want to type in it yourself.", target: '[data-tour="card-view"]', illus: "chatterm" },
  { id: "planner", chapter: "planner", title: "The planner", body: "This is where you talk. The planner starts cards, passes your messages on, and tells you when something needs you.", target: '[data-tour="planner"]', illus: "send" },
  { id: "questions", chapter: "planner", title: "Questions and approvals", body: "When a session needs a decision, you get a question with options. Pick one, add a note if you like, and it carries on.", target: ".qbox", illus: "question" },
  { id: "widgets", chapter: "planner", title: "Live widgets", body: "Sessions explain things with small widgets in chat: tables, charts, diagrams. They follow your theme, and you can pin one to the dashboard.", target: ".widget", illus: "widget" },
  { id: "dashboard", chapter: "automate", title: "The dashboard", body: "One place to watch your work: blocks, card groups and pinned widgets. Next to it, Sessions holds your own terminals. ⌘K finds anything.", target: '[data-tour="views"]', illus: "dash" },
  { id: "integrations", chapter: "automate", title: "Integrations", body: "Pull issues and tickets in as cards. Links and fields stay in sync, and new items can arrive on their own. Set them up in Settings → Integrations.", target: '[data-tour="settings"]', illus: "integrations" },
  { id: "routines", chapter: "automate", title: "Routines", body: "Send a prompt to the planner or a card on a schedule, like a summary every morning. Find them in Settings → Routines.", target: '[data-tour="settings"]', illus: "routines" },
  { id: "mcp-tools", chapter: "mcp", title: "Everything is an MCP tool", body: "Every button in Hadron is also an MCP tool. Any Claude session can drive the board: make and move cards, chat, ask questions, post widgets, set rules.", illus: "mcptools" },
  { id: "mcp-connect", chapter: "mcp", title: "Connect a session", body: "Cards and the planner are connected already. To use Claude Code outside Hadron, add the server from Settings → MCP server.", target: '[data-tour="settings"]', illus: "mcpconnect" },
  { id: "mcp-live", chapter: "mcp", title: "Watch it work", body: "A session calls card_create, and the card appears on your board. hadron_guide is the in-app manual Claude reads before it starts.", illus: "mcplive" },
  { id: "handoff", chapter: "mcp", title: "Let the planner set it up", body: "You don't have to set Hadron up by hand. The planner, and every session in Hadron, can do it for you over MCP: workspaces, columns, cards, integrations, routines, rules, dashboards, card groups and themes.", target: '[data-tour="planner"]', illus: "handoff" },
];

/** The last step's call to action: put in the planner's message box, editable, not sent. */
export const HANDOFF_PROMPT = "Set up Hadron for me: ask me what I work on, then create my workspaces, integrations, routines and a dashboard.";

/** More prompts the last step offers as chips. */
export const HANDOFF_EXAMPLES = [
  "Bring my GitHub issues in as cards and keep them in sync.",
  "Every weekday at 9, summarise what changed overnight.",
  "Make me a dashboard of everything that needs me.",
];

/** The tutorial's sounds; the app plays them through Hadron's player, so the Sounds settings apply. */
export type TourSound =
  | "tour_glide" | "tour_tick" | "tour_pop" | "tour_chime"
  | "tour_swish" | "tour_thud" | "tour_send" | "tour_blip" | "tour_ask" | "tour_confirm" | "tour_sparkle"
  | "tour_ticktock" | "tour_cascade" | "tour_chirp" | "tour_flourish"
  | "tour_ch_basics" | "tour_ch_planner" | "tour_ch_automate" | "tour_ch_mcp";

/**
 * Each illustration's sounds, timed (ms after the step opens) to the first run of its animation.
 * They play once per step, not on every loop.
 */
export const MOCK_SOUNDS: Partial<Record<Illus, [TourSound, number][]>> = {
  board: [["tour_swish", 650], ["tour_thud", 1750]],
  newcard: [["tour_pop", 1400]],
  chatterm: [["tour_swish", 1450]],
  send: [["tour_send", 120], ["tour_blip", 1000]],
  question: [["tour_ask", 150], ["tour_confirm", 1250]],
  widget: [["tour_sparkle", 300]],
  dash: [["tour_cascade", 320]],
  integrations: [["tour_cascade", 1350]],
  routines: [["tour_ticktock", 150], ["tour_send", 900]],
  mcptools: [["tour_chirp", 250]],
  mcpconnect: [["tour_chirp", 350]],
  mcplive: [["tour_chirp", 1750], ["tour_pop", 2050]],
  handoff: [["tour_send", 120], ["tour_blip", 1250], ["tour_flourish", 2250]],
};

/** The sting that opens each chapter; the four share a motif, in a different key. */
export const CHAPTER_STING: Record<ChapterId, TourSound> = {
  basics: "tour_ch_basics", planner: "tour_ch_planner", automate: "tour_ch_automate", mcp: "tour_ch_mcp",
};

const reducedMotion = () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

/** The first step of a chapter. */
export function chapterStart(id: string | undefined, steps: TourStep[] = TOUR_STEPS): number {
  const i = steps.findIndex((s) => s.chapter === id);
  return i < 0 ? 0 : i;
}

/** A window event that opens the tutorial, with detail.chapter to start at a chapter. */
export const TUTORIAL_EVENT = "hadron:tutorial";

/** Opens the tutorial from anywhere in the window, at a chapter or at the start. */
export function showTutorial(chapter?: ChapterId) {
  window.dispatchEvent(new CustomEvent(TUTORIAL_EVENT, { detail: { chapter } }));
}

/** The tutorial's own mute toggle, remembered here. */
export const SETTING_TUTORIAL_MUTED = "tutorial.muted";

/** The ambient pad under the tour: on unless set to false. */
export const SETTING_TUTORIAL_AMBIENT = "tutorial.ambient";

/** Hadron remembers in this setting that the tutorial was seen, finished or skipped. */
export const SETTING_TUTORIAL_SEEN = "tutorial.seen";

interface Box {
  top: number;
  left: number;
  width: number;
  height: number;
}

const PAD = 6;

/** The on-screen box of a step's element, or null when it is missing, hidden or off screen. */
export function targetBox(selector: string | undefined, doc: Document = document): Box | null {
  if (!selector) return null;
  const el = doc.querySelector<HTMLElement>(selector);
  if (!el) return null;
  const r = el.getBoundingClientRect();
  const vw = doc.documentElement.clientWidth || window.innerWidth;
  const vh = doc.documentElement.clientHeight || window.innerHeight;
  if (r.width < 4 || r.height < 4 || r.bottom < 0 || r.right < 0 || r.top > vh || r.left > vw) return null;
  const top = Math.max(4, r.top - PAD);
  const left = Math.max(4, r.left - PAD);
  return { top, left, width: Math.min(vw - 8, r.right + PAD) - left, height: Math.min(vh - 8, r.bottom + PAD) - top };
}

/** Where the caption goes: under the spotlight when it fits, else over it, else centred. */
export function captionPlace(box: Box | null, cap: { w: number; h: number }, view: { w: number; h: number }): { top: number; left: number } {
  const centre = { top: Math.max(16, (view.h - cap.h) / 2), left: Math.max(16, (view.w - cap.w) / 2) };
  if (!box) return centre;
  const left = Math.min(Math.max(16, box.left + box.width / 2 - cap.w / 2), Math.max(16, view.w - cap.w - 16));
  if (box.top + box.height + 14 + cap.h <= view.h - 12) return { top: box.top + box.height + 14, left };
  if (box.top - 14 - cap.h >= 12) return { top: box.top - 14 - cap.h, left };
  // A tall element (a whole panel): beside it, on the side with more room.
  const right = box.left + box.width + 14;
  if (right + cap.w <= view.w - 12) return { top: Math.max(16, Math.min(view.h - cap.h - 16, box.top + 24)), left: right };
  if (box.left - 14 - cap.w >= 12) return { top: Math.max(16, Math.min(view.h - cap.h - 16, box.top + 24)), left: box.left - 14 - cap.w };
  return centre;
}

/** What the MCP steps show of the real server: its address and how many tools it has. */
export interface McpGlance {
  url: string;
  tools: number;
}

const TOOL_NAMES = ["card_create", "card_move", "chat_post", "question_ask", "widget_post", "rule_create", "routine_create", "dash_block_add", "session_start", "link_add"];

/** The step's small animation. big is the mock shown in place of a missing element. */
export function Illustration({ kind, big = false, mcp }: { kind: Illus; big?: boolean; mcp?: McpGlance | null }) {
  const cls = "tour-ill" + (big ? " big" : "");
  const art: Record<Illus, ReactNode> = {
    session: (
      <div className="ti-session">
        <div className="ti-card"><b>billing-fix</b><span>Fix rounding</span></div>
        <div className="ti-arrow">→</div>
        <div className="ti-term"><span>$ claude</span><span className="ti-cursor" /></div>
      </div>
    ),
    tabs: (
      <div className="ti-tabs">
        <span className="on">Acme</span><span>Beta</span><span>Docs</span>
      </div>
    ),
    board: (
      <div className="ti-board">
        <div className="ti-col"><i>Working</i></div>
        <div className="ti-col needs"><i>Needs you</i></div>
        <div className="ti-col"><i>Done</i></div>
        <div className="ti-mover">billing-fix</div>
      </div>
    ),
    newcard: (
      <div className="ti-new">
        <span className="ti-btn">+ New session</span>
        <div className="ti-card ti-pop"><b>search-page</b><span>Speed up search</span></div>
      </div>
    ),
    chatterm: (
      <div className="ti-ct">
        <div className="ti-seg"><span className="ti-knob" /><span>Chat</span><span>Terminal</span></div>
        <div className="ti-ctbody"><span className="ti-line" /><span className="ti-line short" /></div>
      </div>
    ),
    send: (
      <div className="ti-chat">
        <div className="ti-bub them">On it. I started billing-fix.</div>
        <div className="ti-bub me ti-send">Fix the invoice rounding</div>
      </div>
    ),
    question: (
      <div className="ti-q">
        <b>Round negatives how?</b>
        <span className="ti-opt ti-pick">Half away from zero</span>
        <span className="ti-opt">Half up</span>
      </div>
    ),
    widget: (
      <div className="ti-widget">
        <b>Tests by package</b>
        <div className="ti-bars">{[62, 88, 45, 74].map((h, k) => <span key={k} style={{ height: `${h}%`, animationDelay: `${k * 0.15}s` }} />)}</div>
      </div>
    ),
    dash: (
      <div className="ti-dash">
        <div className="ti-blk"><i>In flight</i><span className="ti-num">4</span></div>
        <div className="ti-blk"><i>Needs you</i><span className="ti-num warn">1</span></div>
        <div className="ti-blk wide"><i>Release group</i><span className="ti-chips"><em>billing-fix</em><em>search-page</em></span></div>
      </div>
    ),
    integrations: (
      <div className="ti-integ">
        <div className="ti-ticket"><i>ACME-508</i><span>Invoices round down</span></div>
        <div className="ti-sync">⇄</div>
        <div className="ti-card ti-pop"><b>billing-fix</b><span>ACME-508 · In progress</span></div>
      </div>
    ),
    routines: (
      <div className="ti-rout">
        <div className="ti-clock"><span className="ti-hand" /></div>
        <div className="ti-bub me ti-send">Summarise what changed overnight</div>
        <i className="ti-when">Every weekday at 9:00</i>
      </div>
    ),
    mcptools: (
      <div className="ti-tools">
        {TOOL_NAMES.map((n, k) => <span key={n} style={{ animationDelay: `${k * 0.12}s` }}>{n}</span>)}
      </div>
    ),
    mcpconnect: (
      <div className="ti-connect">
        <i>Settings → MCP server</i>
        <span className="ti-addr mono">{mcp?.url || "http://127.0.0.1:47820/mcp"}</span>
        <span className="ti-when">{mcp?.tools ? `${mcp.tools} tools` : "Every action is a tool"} · cards and the planner are connected already</span>
      </div>
    ),
    mcplive: (
      <div className="ti-live">
        <div className="ti-term ti-call"><span className="ti-typed">card_create {"{"}nick: "search-page"{"}"}</span></div>
        <div className="ti-col"><i>Backlog</i><div className="ti-card ti-drop"><b>search-page</b><span>made by a session</span></div></div>
      </div>
    ),
    handoff: (
      <div className="ti-handoff">
        <div className="ti-bub me ti-h1">Set up Hadron for me</div>
        <div className="ti-bub them ti-h2">On it: a workspace, two cards and a dashboard.</div>
        <div className="ti-made">
          <div className="ti-card ti-h3"><b>billing-fix</b><span>Backlog</span></div>
          <div className="ti-card ti-h4"><b>search-page</b><span>Backlog</span></div>
          <div className="ti-blk ti-h5"><i>Needs you</i><span className="ti-num warn">0</span></div>
        </div>
      </div>
    ),
  };
  return <div className={cls} aria-hidden="true">{art[kind]}</div>;
}

/** The tour. onClose(finished) runs when it ends: finished is false when it was skipped. */
export function Tutorial({ onClose, steps = TOUR_STEPS, start = 0, mcp = null, play, muted = false, onMute, ui, ambient = false, onAmbient }: {
  /** finished is false when the tour was skipped; ask is a prompt for the planner's message box */
  onClose: (finished: boolean, ask?: string) => void;
  steps?: TourStep[];
  start?: number;
  mcp?: McpGlance | null;
  /** plays a sound through Hadron's player (silent when Sounds are off) */
  play?: (s: TourSound) => void;
  muted?: boolean;
  onMute?: (muted: boolean) => void;
  /** the hover and press clicks of the caption's buttons */
  ui?: (kind: "hover" | "press") => void;
  /** the quiet ambient pad under the tour, and its switch */
  ambient?: boolean;
  onAmbient?: (on: boolean) => void;
}) {
  const [i, setI] = useState(Math.min(Math.max(0, start), steps.length - 1));
  const [box, setBox] = useState<Box | null>(null);
  const [place, setPlace] = useState<{ top: number; left: number } | null>(null);
  const cap = useRef<HTMLDivElement>(null);
  const next = useRef<HTMLButtonElement>(null);
  const opener = useRef<Element | null>(typeof document !== "undefined" ? document.activeElement : null);
  const step = steps[i];
  const last = i === steps.length - 1;
  const chapters = CHAPTERS.filter((c) => steps.some((s) => s.chapter === c.id));
  const inChapter = steps.filter((s) => s.chapter === step.chapter);
  const chapterTitle = CHAPTERS.find((c) => c.id === step.chapter)?.title ?? "";

  const sound = useCallback((s: TourSound) => {
    if (!muted && play) play(s);
  }, [muted, play]);
  const measure = useCallback(() => {
    const b = targetBox(step.target);
    setBox(b);
    const c = cap.current;
    const view = { w: window.innerWidth, h: window.innerHeight };
    setPlace(captionPlace(b, { w: c?.offsetWidth || 360, h: c?.offsetHeight || 260 }, view));
  }, [step.target]);

  useLayoutEffect(() => {
    measure();
    // The caption's own size settles after it renders; place it again once it has.
    const t = window.setTimeout(measure, 30);
    window.addEventListener("resize", measure);
    return () => {
      window.clearTimeout(t);
      window.removeEventListener("resize", measure);
    };
  }, [measure, i]);

  useEffect(() => {
    next.current?.focus();
  }, [i]);

  // A chapter opens with its sting; within a chapter, a glide when the spotlight moves, else a tick.
  const prevBox = useRef<Box | null | undefined>(undefined);
  useEffect(() => {
    const before = prevBox.current;
    prevBox.current = box;
    if (i === 0 || steps[i - 1].chapter !== step.chapter || before === undefined) return void sound(CHAPTER_STING[step.chapter]);
    const moved = !!box && (!before || before.top !== box.top || before.left !== box.left || before.width !== box.width);
    sound(moved ? "tour_glide" : "tour_tick");
    // Only the step change should sound, not every remeasure.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [i]);
  // The illustration's own sounds, on its first run. With Reduce motion nothing moves, so nothing
  // plays for the motion.
  useEffect(() => {
    const list = step.illus ? MOCK_SOUNDS[step.illus] : undefined;
    if (!list || reducedMotion()) return;
    const timers = list.map(([s, at]) => window.setTimeout(() => sound(s), at));
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [i, step.illus, sound]);
  // The caption's buttons and chips click softly under the pointer.
  const hovered = useRef<Element | null>(null);
  const click = useCallback((kind: "hover" | "press") => {
    if (!muted && ui) ui(kind);
  }, [muted, ui]);

  const close = useCallback(
    (finished: boolean, ask?: string) => {
      if (finished) sound("tour_chime");
      onClose(finished, ask);
      const back = opener.current as HTMLElement | null;
      if (back && typeof back.focus === "function" && back.isConnected) back.focus();
    },
    [onClose, sound],
  );
  const go = useCallback((d: 1 | -1) => setI((x) => Math.min(steps.length - 1, Math.max(0, x + d))), [steps.length]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close(false);
      else if (e.key === "ArrowRight") last ? close(true) : go(1);
      else if (e.key === "ArrowLeft") go(-1);
      else if (e.key === "Tab") {
        // Keep the focus on the caption's buttons.
        const items = Array.from(cap.current?.querySelectorAll<HTMLElement>("button:not(:disabled)") ?? []);
        if (!items.length) return;
        const at = items.indexOf(document.activeElement as HTMLElement);
        const to = e.shiftKey ? (at <= 0 ? items.length - 1 : at - 1) : at === items.length - 1 ? 0 : at + 1;
        items[to].focus();
      } else return;
      e.preventDefault();
      e.stopPropagation();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [close, go, last]);

  const hole = box ?? { top: window.innerHeight / 2, left: window.innerWidth / 2, width: 0, height: 0 };
  return (
    <div className="tour" data-step={step.id} data-chapter={step.chapter}>
      <div className={"tour-hole" + (box ? "" : " none")} style={{ top: hole.top, left: hole.left, width: hole.width, height: hole.height }} />
      <div
        key={step.id}
        ref={cap}
        className={"tour-cap" + (box ? "" : " centred")}
        role="dialog"
        aria-modal="true"
        aria-labelledby="tour-title"
        aria-describedby="tour-body"
        style={place ? { top: place.top, left: place.left } : { visibility: "hidden" }}
        onPointerOver={(e) => {
          const b = (e.target as Element).closest("button:not(:disabled)");
          if (b && b !== hovered.current) click("hover");
          hovered.current = b;
        }}
        onPointerDown={(e) => {
          if ((e.target as Element).closest("button:not(:disabled)")) click("press");
        }}
      >
        <nav className="tour-chapters" aria-label="Tutorial chapters">
          {chapters.map((c) => (
            <button key={c.id} type="button" className={c.id === step.chapter ? "on" : ""} aria-current={c.id === step.chapter ? "step" : undefined}
              onClick={() => setI(chapterStart(c.id, steps))}>{c.title}</button>
          ))}
        </nav>
        <button type="button" className="ibtn tour-amb" aria-pressed={ambient} disabled={muted} aria-label={ambient ? "Turn the ambient music off" : "Turn the ambient music on"} title={muted ? "Sounds are muted" : ambient ? "Ambient music on" : "Ambient music off"}
          onClick={() => onAmbient?.(!ambient)}><Icon name="wave" size={15} /></button>
        <button type="button" className="ibtn tour-mute" aria-pressed={muted} aria-label={muted ? "Turn the tutorial's sounds on" : "Mute the tutorial's sounds"} title={muted ? "Sounds off" : "Sounds on"}
          onClick={() => onMute?.(!muted)}><Icon name={muted ? "mute" : "speaker"} size={15} /></button>
        <div className="tour-count" aria-live="polite">
          {chapterTitle} · {inChapter.indexOf(step) + 1} of {inChapter.length} <span className="tour-total">(step {i + 1} of {steps.length})</span>
        </div>
        <h3 id="tour-title">{step.title}</h3>
        {step.illus ? <Illustration kind={step.illus} big={!box && !!step.target} mcp={mcp} /> : null}
        <p id="tour-body">{step.body}</p>
        {!box && step.target ? <p className="tour-note">A preview: this part isn’t on screen right now.</p> : null}
        <div className="tour-dots" aria-hidden="true">
          {steps.map((s, k) => <span key={s.id} className={(k === i ? "on" : k < i ? "past" : "") + (k > 0 && steps[k - 1].chapter !== s.chapter ? " gap" : "")} />)}
        </div>
        {step.id === "handoff" ? (
          <div className="tour-asks" role="group" aria-label="Example prompts for the planner">
            {HANDOFF_EXAMPLES.map((p) => (
              <button key={p} type="button" className="tour-ask" onClick={() => close(true, p)}>{p}</button>
            ))}
          </div>
        ) : null}
        <div className="tour-btns">
          <button type="button" className="btn quiet" onClick={() => close(!!last)}>{last ? "I'll explore myself" : "Skip tour"}</button>
          <span style={{ flex: 1 }} />
          <button type="button" className="btn" disabled={i === 0} onClick={() => go(-1)} aria-label="Back">←</button>
          <button type="button" ref={next} className="btn pri" onClick={() => (step.id === "handoff" ? close(true, HANDOFF_PROMPT) : last ? close(true) : go(1))}>
            {step.id === "handoff" ? "Ask the planner →" : last ? "Start using Hadron" : "Next →"}
          </button>
        </div>
      </div>
    </div>
  );
}
