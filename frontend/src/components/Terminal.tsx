import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XTerm } from "@xterm/xterm";
import type { ITheme } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef, useState } from "react";
import * as pty from "../pty";
import type { PtyChunk } from "../pty";
import * as api from "../api";
import { ACCEPT, filesOf, nativeFileDrop, onPathDrop, saveFiles, typedPaths } from "../attach";
import { Icon } from "./ui";

/** The setting that makes Option act as Meta in every terminal (Option+B is ESC b, not ∫). */
export const SETTING_OPTION_META = "terminal.mac_option_is_meta";

/** What a key press in the terminal does. */
export type KeyRoute =
  | "terminal" // xterm turns it into bytes for the program
  | "app" // xterm leaves it alone: the app or the webview acts on it (Cmd+K, Cmd+C, Cmd+V)
  | { send: string }; // Hadron sends these bytes itself

/**
 * Routes a key press in a focused terminal. Everything belongs to the program running in it (so
 * Claude Code gets @, /, Esc, Shift+Tab, Ctrl+C/D/R/L/V, arrows and Tab) except Cmd+letter, which
 * is the app's and the webview's: the palette, a new session, copy, paste, select all.
 * Shift+Enter sends ESC CR, which Claude Code reads as a new line, as Option+Enter does.
 */
export function routeKey(e: Pick<KeyboardEvent, "type" | "key"> & Partial<KeyboardEvent>): KeyRoute {
  if (e.type !== "keydown") return "terminal";
  if (e.key === "Enter" && e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey && !e.isComposing) return { send: "\x1b\r" };
  if (e.metaKey && !e.ctrlKey && /^[a-z]$/i.test(e.key)) return "app";
  return "terminal";
}

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
  const picker = useRef<HTMLInputElement>(null);
  const shiftEsc = useRef(onShiftEscape);
  shiftEsc.current = onShiftEscape;
  const termRef = useRef<XTerm | null>(null);
  const [optionMeta, setOptionMeta] = useState(false);
  const [note, setNote] = useState("");
  const optionMetaRef = useRef(optionMeta);
  optionMetaRef.current = optionMeta;

  // The Option-as-Meta switch is a global setting, so every terminal follows it.
  useEffect(() => {
    let live = true;
    Promise.resolve()
      .then(() => api.settings_get({ key: SETTING_OPTION_META }))
      .then((s) => { if (live) setOptionMeta(s?.[SETTING_OPTION_META] === true); })
      .catch(() => undefined);
    return () => void (live = false);
  }, []);
  useEffect(() => {
    if (termRef.current) termRef.current.options.macOptionIsMeta = optionMeta;
  }, [optionMeta]);

  /** Keeps files and types their paths into the session, as a paste. */
  const attachFiles = async (files: File[]) => {
    if (!files.length) return;
    setNote("Attaching…");
    const { saved, errors } = await saveFiles(files);
    setNote(errors.join(" "));
    if (saved.length) typePaths(saved.map((f) => f.path));
  };
  const typePaths = (paths: string[]) => {
    const term = termRef.current;
    if (!term || !paths.length) return;
    term.paste(typedPaths(paths));
    term.focus();
  };
  const attachRef = useRef(attachFiles);
  attachRef.current = attachFiles;
  const typeRef = useRef(typePaths);
  typeRef.current = typePaths;

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
      macOptionIsMeta: optionMetaRef.current,
      // Option+click moves the cursor in Claude Code's prompt, as in Terminal and iTerm.
      altClickMovesCursor: true,
      theme: termTheme(),
    });
    termRef.current = term;
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
      if (e.type === "keydown" && e.key === "Escape" && e.shiftKey && shiftEsc.current) {
        shiftEsc.current();
        return false;
      }
      const route = routeKey(e);
      if (route === "terminal") return true;
      if (route !== "app") {
        e.preventDefault();
        void pty.write(sessionId, route.send).catch(() => {});
      }
      return false;
    });

    // An image or PDF on the clipboard (Cmd+V) is kept under ~/.hadron/attachments and its path
    // typed, which is how Claude Code attaches an image. Text pastes stay xterm's. Ctrl+V is a key,
    // not a paste: it reaches Claude Code, which reads the clipboard itself.
    const onPaste = (e: ClipboardEvent) => {
      const files = filesOf(e.clipboardData);
      if (!files.length) return;
      e.preventDefault();
      e.stopPropagation();
      void attachRef.current(files);
    };
    el.addEventListener("paste", onPaste, true);
    // Dropped files are typed as their own paths. In the app Wails hands them over as paths; in a
    // plain browser only their bytes exist, so they are kept as attachments first.
    const onDragOver = (e: DragEvent) => {
      if (e.dataTransfer?.types?.includes("Files")) e.preventDefault();
    };
    const onDrop = (e: DragEvent) => {
      if (!e.dataTransfer?.types?.includes("Files")) return;
      e.preventDefault();
      if (!nativeFileDrop()) void attachRef.current(filesOf(e.dataTransfer));
    };
    el.addEventListener("dragover", onDragOver);
    el.addEventListener("drop", onDrop);
    const offDrop = onPathDrop(el, (paths) => typeRef.current(paths));

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
      termRef.current = null;
      el.removeEventListener("paste", onPaste, true);
      el.removeEventListener("dragover", onDragOver);
      el.removeEventListener("drop", onDrop);
      offDrop();
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

  return (
    <>
      <div className="ttools" role="toolbar" aria-label={`${label} terminal tools`}>
        <button className="tbtn" type="button" onClick={() => picker.current?.click()} title="Attach images or PDFs: their paths are typed into the session. You can also paste (Cmd+V) or drop them on the terminal.">
          <Icon name="clip" size={13} />Attach
        </button>
        <input
          ref={picker}
          type="file"
          multiple
          accept={ACCEPT}
          hidden
          aria-label="Choose files to attach to the session"
          onChange={(e) => {
            const list = Array.from(e.target.files ?? []);
            e.target.value = "";
            void attachFiles(list);
          }}
        />
        <label className="tbtn" title="Option+key sends Esc then the key (Meta), for Claude Code's Option shortcuts. Off, Option types special characters such as @ on some layouts.">
          <input
            type="checkbox"
            checked={optionMeta}
            onChange={(e) => {
              const on = e.target.checked;
              setOptionMeta(on);
              void Promise.resolve()
                .then(() => api.settings_update({ values: { [SETTING_OPTION_META]: on } }))
                .catch(() => undefined);
              termRef.current?.focus();
            }}
          />
          Option as Meta
        </label>
        <span className="tnote" role="status">{note}</span>
      </div>
      <div className="xterm-host" ref={host} role="group" aria-label={`${label} terminal`} />
    </>
  );
}
