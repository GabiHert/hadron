// The chats' glass look: Settings → Appearance → Glass chats, solid when macOS asks for reduced
// transparency. Go decides (ui_display_get); this keeps <html data-glass> in step, rereading it
// when settings change and when the window comes back to the front (the macOS setting may have
// changed meanwhile).
import { useEffect } from "react";
import * as api from "../api";

/** The Glass chats setting; it is on unless set to false. */
export const SETTING_GLASS_CHATS = "ui.glass_chats";

/** The Glass transparency slider (0 to 100); 60 unless set. */
export const SETTING_GLASS_TRANSPARENCY = "ui.glass_transparency";
export const DEFAULT_GLASS_TRANSPARENCY = 60;
/** The alpha scale at the slider's most see-through end; Go's themes.MinGlassScale. */
export const MIN_GLASS_SCALE = 0.4;

/** The alpha scale of the glass for a transparency, as Go's themes.GlassScale computes it. */
export function glassScale(transparency: number): number {
  const t = Math.max(0, Math.min(100, transparency));
  return Math.round((1 - ((1 - MIN_GLASS_SCALE) * t) / 100) * 1000) / 1000;
}

/** Sets the glass mode on <html>, which the chat styles key on, and the glass's alpha scale. */
export function applyGlass(mode: api.GlassMode, scale?: number) {
  const el = document.documentElement;
  el.dataset.glass = mode;
  if (scale !== undefined) el.style.setProperty("--glass-k", String(scale));
}

export function useGlass() {
  useEffect(() => {
    let live = true;
    const load = () =>
      api
        .ui_display_get()
        .then((d) => live && d && applyGlass(d.glass, d.glass_scale ?? glassScale(DEFAULT_GLASS_TRANSPARENCY)))
        .catch(() => {});
    // Paint glass at once; the answer only changes it when it is off or solid.
    if (!document.documentElement.dataset.glass) applyGlass("on");
    void load();
    const off = api.onChanged((e) => {
      if (!e.entities?.length || e.entities.includes("settings")) void load();
    });
    window.addEventListener("focus", load);
    return () => {
      live = false;
      off();
      window.removeEventListener("focus", load);
    };
  }, []);
}
