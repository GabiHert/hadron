import { useLayoutEffect } from "react";
import type { RefObject } from "react";

/** How close to the end (px) still counts as reading the newest line. */
const NEAR_END = 48;

/** The conversation that scrolls under the floating header and composer, if it is showing. */
export function overlayScroller(panel: HTMLElement): HTMLElement | null {
  return panel.querySelector<HTMLElement>(".glasswrap > :is(.chat, .cbody)");
}

/** Whether a scroller is at (or within NEAR_END of) its end. */
export function atEnd(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_END;
}

/**
 * Keeps --gh and --gf on a glass panel at the heights of its floating header (.ghead) and composer,
 * so the conversation under them is padded by exactly that much and its newest line is never
 * hidden under the composer. The heights change when the composer grows with its text or
 * attachments, when the header wraps, and when the panel resizes; when they do, a conversation that
 * was showing its newest line is pinned to it again (the chat's own follow only reacts to new lines
 * and to the lines' sizes).
 */
export function useGlassOverlay(ref: RefObject<HTMLElement | null>, active: boolean, key = "") {
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !active) return;
    const set = () => {
      const head = el.querySelector<HTMLElement>(":scope > .ghead");
      const foot = el.querySelector<HTMLElement>(":scope > .composer");
      const gh = `${head?.offsetHeight ?? 0}px`;
      const gf = `${foot?.offsetHeight ?? 0}px`;
      if (el.style.getPropertyValue("--gh") === gh && el.style.getPropertyValue("--gf") === gf) return;
      const sc = overlayScroller(el);
      // A first measure (nothing set yet) always lands on the newest line, as the chat opens there.
      const pin = !!sc && (!el.style.getPropertyValue("--gf") || atEnd(sc));
      el.style.setProperty("--gh", gh);
      el.style.setProperty("--gf", gf);
      if (sc && pin) sc.scrollTop = sc.scrollHeight;
    };
    set();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(set);
    const watch = () => {
      ro.disconnect();
      ro.observe(el);
      for (const c of Array.from(el.children)) if (c.matches(".ghead, .composer")) ro.observe(c);
      set();
    };
    watch();
    // The composer mounts and unmounts with the view and the tab.
    const mo = typeof MutationObserver === "undefined" ? null : new MutationObserver(watch);
    mo?.observe(el, { childList: true });
    return () => {
      ro.disconnect();
      mo?.disconnect();
    };
  }, [ref, active, key]);
}
