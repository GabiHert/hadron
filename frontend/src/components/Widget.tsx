// Widgets are HTML a planner or a session wrote. They run in an iframe with a null origin:
//   - `srcDoc` only, never `src`;
//   - `sandbox` is "" (no scripts) or "allow-scripts", and never has allow-same-origin;
//   - a CSP meta denies everything but inline styles and data: images (inline scripts only when allowed).
// Scripts are off by default. The setting `widgets.scripts` (true/false, set with settings_update)
// allows them for every widget.
import { useEffect, useRef, useState } from "react";
import tokensCss from "../styles/tokens.css?raw";

export const WIDGET_MIN_HEIGHT = 80;
export const WIDGET_MAX_HEIGHT = 1200;
export const WIDGET_DEFAULT_HEIGHT = 220;

const CSP_BASE = "default-src 'none'; style-src 'unsafe-inline'; img-src data:";
export const widgetCsp = (scripts: boolean) => (scripts ? `${CSP_BASE}; script-src 'unsafe-inline'` : CSP_BASE);
export const widgetSandbox = (scripts: boolean) => (scripts ? "allow-scripts" : "");
export const clampHeight = (h: number) => Math.min(WIDGET_MAX_HEIGHT, Math.max(WIDGET_MIN_HEIGHT, Math.round(h) || WIDGET_DEFAULT_HEIGHT));

// Token names and their light-theme defaults, from the first :root block of tokens.css. The live
// (theme-aware) value is read from the host document; the default is the fallback when none is set.
const DEFAULTS = new Map(Array.from((tokensCss.match(/:root\s*\{([^}]*)\}/)?.[1] ?? "").matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g), (m) => [m[1], m[2].trim()] as const));
const TOKEN_NAMES = Array.from(DEFAULTS.keys());

function themeCss(): string {
  const cs = typeof document !== "undefined" ? getComputedStyle(document.documentElement) : null;
  const decls = TOKEN_NAMES.map((n) => [n, cs?.getPropertyValue(n).trim() || DEFAULTS.get(n) || ""] as const)
    .filter(([, v]) => v !== "" && !/[<>{}]/.test(v))
    .map(([n, v]) => `${n}:${v};`);
  return `:root{${decls.join("")}}body{margin:0;font:13px/1.5 system-ui,sans-serif;color:var(--fg);background:transparent}`;
}

const HEIGHT_SCRIPT =
  "<script>(function(){function p(){parent.postMessage({hadronHeight:document.documentElement.scrollHeight},'*')}" +
  "addEventListener('load',p);addEventListener('resize',p);if(window.ResizeObserver)new ResizeObserver(p).observe(document.documentElement)})()</script>";

/** The full document an iframe shows. `data` becomes <script type="application/json" id="hadron-data">. */
export function widgetSrcDoc(html: string, opts: { scripts: boolean; data?: unknown }): string {
  const head = `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${widgetCsp(opts.scripts)}">`;
  const style = `<style>${themeCss()}</style>`;
  const json = opts.data === undefined ? "" : `<script type="application/json" id="hadron-data">${JSON.stringify(opts.data).replace(/</g, "\\u003c")}</script>`;
  return head + style + json + html + (opts.scripts ? HEIGHT_SCRIPT : "");
}

interface Props {
  title: string;
  html: string;
  height?: number;
  data?: unknown;
  scripts: boolean;
  onPin?: () => void;
  pinned?: boolean;
}

export function Widget({ title, html, height, data, scripts, onPin, pinned }: Props) {
  const frame = useRef<HTMLIFrameElement>(null);
  const [auto, setAuto] = useState<number | null>(null);
  useEffect(() => {
    if (!scripts) return;
    const on = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow) return;
      const h = (e.data as { hadronHeight?: unknown } | null)?.hadronHeight;
      if (typeof h === "number" && Number.isFinite(h)) setAuto(clampHeight(h));
    };
    window.addEventListener("message", on);
    return () => window.removeEventListener("message", on);
  }, [scripts]);
  const h = clampHeight((scripts && auto) || height || WIDGET_DEFAULT_HEIGHT);
  return (
    <div className="widget" role="group" aria-label={`Widget ${title}`}>
      {title || onPin ? (
        <div className="wh">
          <b style={{ flex: 1 }}>{title}</b>
          {onPin ? (
            <button className="btn sm" type="button" disabled={pinned} aria-label={pinned ? `${title} is on the dashboard` : `Pin ${title} to the dashboard`} onClick={onPin}>
              {pinned ? "On the dashboard" : "Pin to dashboard"}
            </button>
          ) : null}
        </div>
      ) : null}
      <iframe
        ref={frame}
        title={title || "Widget"}
        sandbox={widgetSandbox(scripts)}
        srcDoc={widgetSrcDoc(html, { scripts, data })}
        style={{ width: "100%", height: h, border: 0, display: "block", background: "transparent" }}
      />
    </div>
  );
}
