import { useRef } from "react";
import type { KeyboardEvent, PointerEvent } from "react";
import { KEY_STEP, KEY_STEP_BIG, PANEL_LIMITS } from "../state/panelLayout";
import type { PanelControls, PanelName } from "../state/panelLayout";

const LABEL: Record<PanelName, string> = { planner: "Resize the planner", card: "Resize the card panel" };

/**
 * The drag handle on a panel's inner edge (the planner's right, the card panel's left). Drag it,
 * or focus it and use ← and → (⇧ for bigger steps), Home and End; double-click it or press Enter
 * to go back to the default width.
 */
export function PanelResizer({ panel, ctl, zoom = 1 }: { panel: PanelName; ctl: PanelControls; zoom?: number }) {
  const drag = useRef<{ x: number; w: number; id: number; last: number; frame: number } | null>(null);
  const { width, min, max } = ctl.slot;
  // Dragging toward the board widens the panel: right for the planner, left for the card panel.
  const dir = panel === "planner" ? 1 : -1;

  const end = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    cancelAnimationFrame(d.frame);
    drag.current = null;
    document.documentElement.removeAttribute("data-resizing");
    try {
      e.currentTarget.releasePointerCapture?.(d.id);
    } catch {
      /* already released */
    }
    if (d.last !== d.w) ctl.resize(d.last, true);
  };

  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.shiftKey ? KEY_STEP_BIG : KEY_STEP;
    let next: number | null = null;
    if (e.key === "ArrowRight") next = width + step * dir;
    else if (e.key === "ArrowLeft") next = width - step * dir;
    else if (e.key === "Home") next = min;
    else if (e.key === "End") next = max;
    else if (e.key === "Enter") {
      e.preventDefault();
      ctl.reset();
      return;
    }
    if (next === null) return;
    e.preventDefault();
    e.stopPropagation();
    ctl.resize(next, true);
  };

  return (
    <div
      className={"presize presize-" + panel}
      role="separator"
      aria-orientation="vertical"
      aria-label={LABEL[panel]}
      aria-valuenow={width}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuetext={`${width} pixels wide`}
      tabIndex={0}
      title={`Drag to resize; double-click for the default width (${PANEL_LIMITS[panel].def} px). With the keyboard: ← → (⇧ for bigger steps), Home, End, ↩ to reset.`}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        try {
          e.currentTarget.setPointerCapture?.(e.pointerId);
        } catch {
          /* no capture in this environment */
        }
        drag.current = { x: e.clientX, w: width, id: e.pointerId, last: width, frame: 0 };
        document.documentElement.setAttribute("data-resizing", "");
      }}
      onPointerMove={(e) => {
        const d = drag.current;
        if (!d) return;
        // clientX is in window px; the panels are laid out in the zoomed page's px.
        d.last = Math.round(Math.max(min, Math.min(max, d.w + ((e.clientX - d.x) * dir) / (zoom || 1))));
        cancelAnimationFrame(d.frame);
        const w = d.last;
        d.frame = requestAnimationFrame(() => ctl.resize(w, false));
      }}
      onPointerUp={end}
      onPointerCancel={end}
      onLostPointerCapture={end}
      onDoubleClick={() => ctl.reset()}
      onKeyDown={key}
    />
  );
}
