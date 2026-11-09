import { useEffect, useRef } from "react";
import * as api from "../api";
import { runCommand } from "../commands";
import type { Env } from "../commands";
import { Dispatcher, byId, contextOf } from "../keys";
import type { Overrides } from "../keys";

type Source = "page" | "menu";

/** The event the native View, Go and Card menus send, with the command id. */
export const EVENT_MENU_COMMAND = "menu:command";

/** True while a dialog (the palette, Settings, a confirm) is open. */
export const modalOpen = () => !!document.querySelector('[aria-modal="true"]');

/**
 * One keyboard for the whole window: a keydown listener runs the registry's dispatcher, and the
 * native menus' clicks run the same commands. The menu accelerators and the page see the same ⌘
 * chords; whichever fires first wins and the other is ignored for a moment, so a chord never runs twice.
 */
export function useKeyboard(env: Env, overrides: Overrides, extra?: (id: string, arg?: string) => boolean | void) {
  const envRef = useRef(env);
  envRef.current = env;
  const oRef = useRef(overrides);
  oRef.current = overrides;
  const extraRef = useRef(extra);
  extraRef.current = extra;
  const last = useRef<{ id: string; at: number; from: Source }>({ id: "", at: 0, from: "page" });

  const run = (id: string, arg?: string, from: Source = "page") => {
    // The same chord seen by the page and by a native menu accelerator: only the first one runs.
    const l = last.current;
    if (l.id === id && l.from !== from && Date.now() - l.at < 400) return true;
    const x = extraRef.current?.(id, arg);
    if (x !== undefined) return x;
    const out = runCommand(envRef.current, id, arg);
    if (out !== false) last.current = { id, at: Date.now(), from };
    return out;
  };
  const runRef = useRef(run);
  runRef.current = run;

  useEffect(() => {
    const d = new Dispatcher(
      () => oRef.current,
      (t) => contextOf(t, modalOpen(), envRef.current.board.ws?.ui.main_view === "dashboard" ? "dashboard" : "board"),
      (id, arg) => runRef.current(id, arg),
    );
    const onKey = (e: KeyboardEvent) => {
      if (d.handle(e)) {
        e.preventDefault();
        e.stopPropagation();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // The native menus. A menu accelerator the page already handled arrives here too, at most a
  // moment later: skip it. With a dialog open only the commands that work there run.
  useEffect(
    () =>
      api.onEvent(EVENT_MENU_COMMAND, (data) => {
        const id = typeof data === "string" ? data : "";
        const def = byId.get(id);
        if (!def) return;
        if (modalOpen() && !def.dialog) return;
        runRef.current(id, undefined, "menu");
      }),
    [],
  );

  return (id: string, arg?: string) => run(id, arg, "page");
}

/** The panel the user worked in last, updated as the focus moves. */
export function useLastPanel() {
  const last = useRef<"planner" | "card" | null>(null);
  useEffect(() => {
    const on = (e: FocusEvent) => {
      const z = e.target instanceof Element ? e.target.closest("[data-zone]")?.getAttribute("data-zone") : null;
      if (z === "planner" || z === "card") last.current = z;
    };
    document.addEventListener("focusin", on);
    return () => document.removeEventListener("focusin", on);
  }, []);
  return last;
}
