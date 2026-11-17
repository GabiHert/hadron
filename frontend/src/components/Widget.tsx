// Widgets are HTML a planner or a session wrote. They run in an iframe with a null origin:
//   - `srcDoc` only, never `src`;
//   - `sandbox` is "allow-scripts" and never has allow-same-origin;
//   - a CSP meta denies everything but inline styles and data: images. Scripts run only when the
//     setting `widgets.scripts` (true/false, set with settings_update) allows them; otherwise the CSP
//     allows one script, Hadron's own, by a nonce made per frame: it reports the content's height.
//
// Every frame starts with Hadron's base styles, before the widget's own: the app's colour scheme
// and theme tokens as real values, an explicit background and text colour, wrapping text and tables,
// and the widget kit (styles/widgetkit.css), ready classes built on the tokens. The tokens follow a
// live theme switch.
//
// In a chat a widget is part of the conversation: a quiet caption (only when it has a title), the
// frame, and a "⋯" menu that shows on hover or keyboard focus with Open full screen, Copy HTML and,
// when the chat can pin, Add to dashboard. Pinning is the user's choice, never the default action.
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, Ref } from "react";
import { createPortal } from "react-dom";
import tokensCss from "../styles/tokens.css?raw";
import kitCss from "../styles/widgetkit.css?raw";
import { Icon } from "./ui";
import { THEME_EVENT } from "../theme";

export const WIDGET_MIN_HEIGHT = 80;
export const WIDGET_MAX_HEIGHT = 1200;
export const WIDGET_DEFAULT_HEIGHT = 220;
/** The tallest a guessed height gets; "Show more" goes past it. */
export const WIDGET_GUESS_MAX = 480;

const CSP_BASE = "default-src 'none'; style-src 'unsafe-inline'; img-src data:";
/** The frame's CSP: the widget's own scripts when allowed, else only Hadron's script with `nonce`. */
export const widgetCsp = (scripts: boolean, nonce = "") =>
  scripts ? `${CSP_BASE}; script-src 'unsafe-inline'` : nonce ? `${CSP_BASE}; script-src 'nonce-${nonce}'` : CSP_BASE;
/** Scripts are always allowed by the sandbox; the CSP decides which run. */
export const widgetSandbox = (_scripts: boolean) => "allow-scripts";
export const clampHeight = (h: number) => Math.min(WIDGET_MAX_HEIGHT, Math.max(WIDGET_MIN_HEIGHT, Math.round(h) || WIDGET_DEFAULT_HEIGHT));
/** A height the frame measured itself: it may be small, never taller than the cap. */
export const clampReported = (h: number) => Math.min(WIDGET_MAX_HEIGHT, Math.max(16, Math.ceil(h)));

const SVG_OPEN = /^\s*<svg\b[^>]*>/i;

/** Whether a widget is a diagram (it starts with an <svg>), which gets the full chat width. */
export const isDiagram = (html: string) => SVG_OPEN.test(html);

/**
 * A height for a widget whose frame cannot report its own (scripts off). An SVG that starts the
 * widget is sized exactly from its height, or from its viewBox and the frame's width. Anything
 * else is guessed from its rows and length, capped at WIDGET_GUESS_MAX. `exact` says which.
 */
export function estimateHeight(html: string, width: number): { height: number; exact: boolean } {
  if (html.trim() === "") return { height: WIDGET_DEFAULT_HEIGHT, exact: false };
  const open = html.match(SVG_OPEN)?.[0];
  if (open) {
    const num = (attr: string) => {
      const m = open.match(new RegExp(`\\s${attr}\\s*=\\s*["']?\\s*(\\d+(?:\\.\\d+)?)\\s*(px)?["'\\s/>]`, "i"));
      return m ? Number(m[1]) : NaN;
    };
    const h = num("height");
    if (Number.isFinite(h)) return { height: clampHeight(h + 8), exact: true };
    const vb = open.match(/viewBox\s*=\s*["']\s*-?[\d.]+[\s,]+-?[\d.]+[\s,]+([\d.]+)[\s,]+([\d.]+)/i);
    if (vb && Number(vb[1]) > 0 && width > 0) {
      const w = num("width");
      const shown = Number.isFinite(w) ? Math.min(w, width) : width;
      return { height: clampHeight((shown * Number(vb[2])) / Number(vb[1]) + 8), exact: true };
    }
  }
  const rows = (html.match(/<(tr|li|p|h[1-6]|br|div|section|header|footer|dt|dd)\b/gi) ?? []).length;
  const text = html.replace(/<style[\s\S]*?<\/style>|<script[\s\S]*?<\/script>|<[^>]*>/gi, "").replace(/\s+/g, " ").trim().length;
  const lines = Math.max(1, rows, Math.ceil(text / Math.max(40, width / 7)));
  return { height: Math.min(WIDGET_GUESS_MAX, clampHeight(20 + lines * 22)), exact: false };
}

// Token names and their light-theme defaults, from the first :root block of tokens.css. The live
// (theme-aware) value is read from the host document; the default is the fallback when none is set.
const DEFAULTS = new Map(Array.from((tokensCss.match(/:root\s*\{([^}]*)\}/)?.[1] ?? "").matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g), (m) => [m[1], m[2].trim()] as const));
const TOKEN_NAMES = Array.from(DEFAULTS.keys()).filter((n) => n !== "--shadow" && n !== "--scrim");

export interface WidgetTheme {
  mode: "light" | "dark";
  /** every token, such as "--fg": "#ece8df", and the font stacks --sans, --head and --mono */
  tokens: Record<string, string>;
}

/** The app's theme now: its mode (data-theme, else macOS) and every token's live value. */
export function widgetTheme(): WidgetTheme {
  const root = typeof document !== "undefined" ? document.documentElement : null;
  const set = root?.getAttribute("data-theme");
  const mode = set === "dark" || set === "light" ? set : typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ? "dark" : "light";
  const cs = root ? getComputedStyle(root) : null;
  const tokens: Record<string, string> = {};
  for (const n of TOKEN_NAMES) {
    const v = cs?.getPropertyValue(n).trim() || DEFAULTS.get(n) || "";
    if (v !== "" && !/[<>{};]/.test(v)) tokens[n] = v;
  }
  return { mode, tokens };
}

/**
 * The styles every frame starts with, before the widget's own. Every var() has a system-colour
 * fallback, and color-scheme matches the app so the frame never paints a white canvas in dark mode.
 */
export function widgetBaseCss(theme: WidgetTheme): string {
  const decls = Object.entries(theme.tokens).map(([n, v]) => `${n}:${v};`).join("");
  return (
    `:root{color-scheme:${theme.mode};${decls}}` +
    "*,*::before,*::after{box-sizing:border-box}" +
    "html{overflow-x:auto}" +
    "body{margin:0;padding:8px 10px;display:flow-root;background:var(--card,Canvas);color:var(--fg,CanvasText);font:13px/1.5 var(--sans,-apple-system,system-ui,sans-serif);overflow-wrap:break-word;-webkit-text-size-adjust:100%}" +
    "img,svg,video,canvas{max-width:100%}img,video{height:auto}" +
    "a{color:var(--acc,LinkText)}hr{border:0;border-top:1px solid var(--border,GrayText);margin:8px 0}" +
    "pre{overflow-x:auto;max-width:100%}" +
    "table{width:100%;max-width:100%;border-collapse:collapse}" +
    "th,td{padding:4px 6px;text-align:left;vertical-align:top;overflow-wrap:anywhere;color:inherit}" +
    "th{color:var(--fg2,CanvasText)}"
  );
}

/** A random token for the CSP nonce; one per frame. */
export function makeNonce(): string {
  const a = new Uint8Array(12);
  globalThis.crypto?.getRandomValues?.(a);
  if (a.every((x) => x === 0)) for (let i = 0; i < a.length; i++) a[i] = Math.floor(Math.random() * 256);
  return Array.from(a, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** Re-renders when the theme changes, so a widget's copied tokens follow it. */
function useThemeVersion() {
  const [, bump] = useState(0);
  useEffect(() => {
    const on = () => bump((n) => n + 1);
    window.addEventListener(THEME_EVENT, on);
    window.matchMedia?.("(prefers-color-scheme: dark)")?.addEventListener?.("change", on);
    return () => {
      window.removeEventListener(THEME_EVENT, on);
      window.matchMedia?.("(prefers-color-scheme: dark)")?.removeEventListener?.("change", on);
    };
  }, []);
}

// Reports the content's height (the body, plus a horizontal scrollbar if one shows) to the host.
const HEIGHT_JS =
  "(function(){var d=document,last=-1;function p(){var b=d.body;if(!b)return;" +
  "var h=Math.ceil(Math.max(b.scrollHeight,b.getBoundingClientRect().height))+Math.max(0,innerHeight-d.documentElement.clientHeight);" +
  "if(h!==last){last=h;parent.postMessage({hadronHeight:h},'*')}}" +
  "addEventListener('load',p);addEventListener('resize',p);if(window.ResizeObserver)new ResizeObserver(p).observe(d.body);" +
  "if(d.fonts&&d.fonts.ready)d.fonts.ready.then(p);p()})()";

const jsonInScript = (v: unknown) => JSON.stringify(v ?? null).replace(/</g, "\\u003c");

/**
 * The full document an iframe shows. `data` becomes <script type="application/json" id="hadron-data">,
 * and the theme <script type="application/json" id="hadron-theme"> ({mode, tokens}).
 */
export function widgetSrcDoc(html: string, opts: { scripts: boolean; data?: unknown; nonce?: string; theme?: WidgetTheme }): string {
  const theme = opts.theme ?? widgetTheme();
  const nonce = opts.nonce ?? "";
  const head =
    `<!doctype html><html data-theme="${theme.mode}"><head><meta charset="utf-8">` +
    `<meta http-equiv="Content-Security-Policy" content="${widgetCsp(opts.scripts, nonce)}">` +
    `<style id="hadron-base">${widgetBaseCss(theme)}</style><style id="hadron-kit">${kitCss}</style>` +
    `<script type="application/json" id="hadron-theme">${jsonInScript(theme)}</script>` +
    (opts.data === undefined ? "" : `<script type="application/json" id="hadron-data">${jsonInScript(opts.data)}</script>`) +
    "</head><body>";
  const measure = opts.scripts || nonce ? `<script${nonce ? ` nonce="${nonce}"` : ""}>${HEIGHT_JS}</script>` : "";
  return head + html + measure;
}

interface Props {
  title: string;
  html: string;
  height?: number;
  data?: unknown;
  scripts: boolean;
  /** Adds "Add to dashboard" to the menu. Only a chat passes it. */
  onPin?: () => void;
  pinned?: boolean;
  /** "chat" (the default) has the menu and the conversation's widths; "block" is a dashboard block's body. */
  variant?: "chat" | "block";
}

function Frame({ title, srcDoc, scripts, height, frameRef }: { title: string; srcDoc: string; scripts: boolean; height?: number | string; frameRef?: Ref<HTMLIFrameElement> }) {
  return (
    <iframe
      ref={frameRef}
      title={title || "Widget"}
      sandbox={widgetSandbox(scripts)}
      srcDoc={srcDoc}
      style={{ width: "100%", height: height ?? "100%", border: 0, display: "block", background: "transparent" }}
    />
  );
}

/** The widget at a large width, over the app. Esc, the Close button or a click outside closes it. */
function FullScreen({ title, srcDoc, scripts, onClose }: { title: string; srcDoc: string; scripts: boolean; onClose: () => void }) {
  const id = useId();
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    close.current?.focus();
    const on = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    document.addEventListener("keydown", on, true);
    return () => document.removeEventListener("keydown", on, true);
  }, [onClose]);
  return createPortal(
    <div className="scrim wfull-scrim" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="modal wide wfull" role="dialog" aria-modal="true" aria-labelledby={id}>
        <div className="mh">
          <h3 id={id}>{title || "Widget"}</h3>
          <button ref={close} className="ibtn" type="button" onClick={onClose} aria-label="Close"><Icon name="close" /></button>
        </div>
        <div className="wfull-body">
          <Frame title={title} srcDoc={srcDoc} scripts={scripts} />
        </div>
      </div>
    </div>,
    document.body,
  );
}

function WidgetMenu({ title, html, onPin, pinned, onFull }: { title: string; html: string; onPin?: () => void; pinned?: boolean; onFull: () => void }) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const menuId = useId();
  useEffect(() => {
    if (!open) return;
    root.current?.querySelector<HTMLButtonElement>('[role="menuitem"]:not([disabled])')?.focus();
    const away = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open]);
  const shut = () => {
    setOpen(false);
    setCopied(false);
    button.current?.focus();
  };
  const onKey = (e: ReactKeyboardEvent) => {
    if (e.key === "Escape" && open) {
      e.preventDefault();
      e.stopPropagation();
      shut();
      return;
    }
    if (!open || (e.key !== "ArrowDown" && e.key !== "ArrowUp")) return;
    e.preventDefault();
    const items = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ?? []);
    const at = items.indexOf(document.activeElement as HTMLButtonElement);
    items[(at + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]?.focus();
  };
  const name = title || "this widget";
  return (
    <div className="wmenu" ref={root} onKeyDown={onKey}>
      <button
        ref={button}
        type="button"
        className="wmenu-btn"
        aria-label={`More for ${name}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => (open ? shut() : setOpen(true))}
      >
        <Icon name="dots" size={15} />
      </button>
      {open ? (
        <div className="wmenu-list" role="menu" id={menuId} aria-label={`Actions for ${name}`}>
          <button type="button" role="menuitem" onClick={() => { shut(); onFull(); }}>Open full screen</button>
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              void navigator.clipboard?.writeText(html).then(() => setCopied(true), () => undefined);
            }}
          >
            {copied ? "Copied" : "Copy HTML"}
          </button>
          {onPin ? (
            <button type="button" role="menuitem" disabled={pinned} aria-disabled={pinned || undefined} onClick={() => { shut(); onPin(); }}>
              {pinned ? "On the dashboard ✓" : "Add to dashboard"}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function Widget({ title, html, height, data, scripts, onPin, pinned, variant = "chat" }: Props) {
  const frame = useRef<HTMLIFrameElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const [auto, setAuto] = useState<number | null>(null);
  const [width, setWidth] = useState(0);
  const [more, setMore] = useState(false);
  const [full, setFull] = useState(false);
  const [nonce] = useState(makeNonce);
  useEffect(() => {
    const on = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow) return;
      const h = (e.data as { hadronHeight?: unknown } | null)?.hadronHeight;
      if (typeof h === "number" && Number.isFinite(h)) setAuto(clampReported(h));
    };
    window.addEventListener("message", on);
    return () => window.removeEventListener("message", on);
  }, []);
  // The frame's width, for sizing an SVG from its viewBox.
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    setWidth(el.clientWidth);
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const guess = estimateHeight(html, width || 480);
  const reported = auto !== null;
  const canGrow = !reported && !height && !guess.exact;
  const h = auto !== null ? auto : height ? clampHeight(height) : more && canGrow ? clampHeight(Math.max(guess.height * 2, 600)) : guess.height;
  useThemeVersion();
  const srcDoc = widgetSrcDoc(html, { scripts, data, nonce });
  const chat = variant === "chat";
  const cls = ["widget", chat ? "w-chat" : "w-block", chat && isDiagram(html) ? "w-wide" : ""].filter(Boolean).join(" ");
  return (
    <div className={cls} role="group" aria-label={`Widget ${title}`.trim()} {...(chat ? { tabIndex: -1, "data-msg": "" } : {})}>
      {chat ? <WidgetMenu title={title} html={html} onPin={onPin} pinned={pinned} onFull={() => setFull(true)} /> : null}
      {title ? <div className="wcap">{title}</div> : null}
      <div className="wframe" ref={box}>
        <Frame title={title} srcDoc={srcDoc} scripts={scripts} height={h} frameRef={frame} />
      </div>
      {canGrow ? (
        <button type="button" className="wmore" aria-expanded={more} onClick={() => setMore((v) => !v)}>
          {more ? "Show less" : "Show more"}
        </button>
      ) : null}
      {full ? <FullScreen title={title} srcDoc={srcDoc} scripts={scripts} onClose={() => setFull(false)} /> : null}
    </div>
  );
}
