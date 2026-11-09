import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XTerm } from "@xterm/xterm";
import type { ITheme } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";
import * as pty from "../pty";
import type { PtyChunk } from "../pty";

/** The terminal palette, read from the design tokens so it follows the theme. */
export function termTheme(): ITheme {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;
  const fg = v("--tfg", "#ece8df");
  return {
    background: v("--term", "#151412"),
    foreground: fg,
    cursor: fg,
    cursorAccent: v("--term", "#151412"),
    selectionBackground: "rgba(143, 163, 232, 0.35)",
    red: v("--tr", "#f0877d"),
    green: v("--tg", "#7fcf9b"),
    yellow: v("--ty", "#e6b65c"),
    blue: v("--tb", "#8fa3e8"),
    brightBlack: v("--tdim", "#9c9da6"),
    brightWhite: fg,
  };
}

interface Props {
  /** The session to show. Changing it mounts a fresh terminal. */
  sessionId: string;
  label: string;
  /** Shift+Esc in the terminal (plain Esc belongs to the program running in it). */
  onShiftEscape?: () => void;
}

/**
 * xterm.js over a session's PTY. On mount it subscribes to the output events, replays the
 * scrollback (PtyAttach), and writes events that arrive during the attach after it, trimmed to the
 * bytes the replay does not already hold. Typing goes to PtyWrite and size changes to PtyResize.
 */
export function Terminal({ sessionId, label, onShiftEscape }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const shiftEsc = useRef(onShiftEscape);
  shiftEsc.current = onShiftEscape;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let dead = false;
    const term = new XTerm({
      fontFamily: '"IBM Plex Mono", ui-monospace, Menlo, monospace',
      fontSize: 12,
      lineHeight: 1.3,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: false,
      theme: termTheme(),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    const refit = () => {
      if (dead) return;
      try {
        fit.fit();
      } catch {
        /* the element has no size yet (hidden); the observer fits it when it gets one */
      }
    };
    refit();

    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== "keydown") return true;
      if (e.key === "Escape" && e.shiftKey && shiftEsc.current) {
        shiftEsc.current();
        return false;
      }
      // Cmd+letter is for the app (palette, new session, copy, paste), never for the program.
      if (e.metaKey && /^[a-z]$/i.test(e.key)) return false;
      return true;
    });

    // Live output is queued until the replay is written, then applied in order.
    let pos = -1;
    const queue: PtyChunk[] = [];
    const apply = (c: PtyChunk) => {
      if (c.end <= pos) return;
      const bytes = pty.bytesFromB64(c.data);
      term.write(c.start < pos ? bytes.subarray(pos - c.start) : bytes);
      pos = c.end;
    };
    const off = pty.onData(sessionId, (c) => {
      if (dead) return;
      if (pos < 0) queue.push(c);
      else apply(c);
    });
    void pty
      .attach(sessionId)
      .then(({ end, bytes }) => {
        if (dead) return;
        if (bytes.length) term.write(bytes);
        pos = end;
        queue.splice(0).forEach(apply);
        void pty.resize(sessionId, term.cols, term.rows).catch(() => {});
      })
      .catch(() => {});

    const typed = term.onData((data) => void pty.write(sessionId, data).catch(() => {}));
    const resized = term.onResize(({ cols, rows }) => void pty.resize(sessionId, cols, rows).catch(() => {}));

    let frame = 0;
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(refit);
    });
    ro.observe(el);

    // The theme tokens change with data-theme and with the OS setting.
    const retheme = () => {
      term.options.theme = termTheme();
    };
    const mo = new MutationObserver(retheme);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener?.("change", retheme);
    // Plex Mono may load after the first measure; measure again when it has.
    void document.fonts?.ready.then(refit);

    term.focus();
    return () => {
      dead = true;
      off();
      typed.dispose();
      resized.dispose();
      ro.disconnect();
      mo.disconnect();
      media?.removeEventListener?.("change", retheme);
      cancelAnimationFrame(frame);
      term.dispose();
    };
  }, [sessionId]);

  return <div className="xterm-host" ref={host} role="group" aria-label={`${label} terminal`} />;
}
