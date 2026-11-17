// With glass on, a panel's header and composer float over its chat, which scrolls beneath them.
// The chat needs room at the top and bottom for them, so this keeps --gh and --gf on the panel at
// the header's and the composer's heights.
import { useLayoutEffect } from "react";
import type { RefObject } from "react";

export function useGlassOverlay(ref: RefObject<HTMLElement | null>, active: boolean, key = "") {
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !active) return;
    const set = () => {
      const head = el.querySelector<HTMLElement>(":scope > .ghead");
      const foot = el.querySelector<HTMLElement>(":scope > .composer");
      el.style.setProperty("--gh", `${head?.offsetHeight ?? 0}px`);
      el.style.setProperty("--gf", `${foot?.offsetHeight ?? 0}px`);
    };
    set();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(set);
    const watch = () => {
      ro.disconnect();
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
