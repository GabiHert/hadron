// The chats' glass look: Settings → Appearance → Glass chats, solid when macOS asks for reduced
// transparency. Go decides (ui_display_get); this keeps <html data-glass> in step, rereading it
// when settings change and when the window comes back to the front (the macOS setting may have
// changed meanwhile).
import { useEffect } from "react";
import * as api from "../api";

/** The Glass chats setting; it is on unless set to false. */
export const SETTING_GLASS_CHATS = "ui.glass_chats";

/** Sets the glass mode on <html>, which the chat styles key on. */
export function applyGlass(mode: api.GlassMode) {
  document.documentElement.dataset.glass = mode;
}

export function useGlass() {
  useEffect(() => {
    let live = true;
    const load = () =>
      api
        .ui_display_get()
        .then((d) => live && d && applyGlass(d.glass))
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
