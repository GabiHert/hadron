import { ZOOM_MAX, ZOOM_MIN } from "../commands";
import { chordLabel } from "../keys";
import type { Board, Theme } from "../state/useBoard";
import { useZoom } from "../state/useZoom";

/** Settings → Appearance: light or dark, and the zoom (⌘= ⌘- ⌘0) that scales the UI and the terminals. */
export function AppearanceTab({ board }: { board: Board }) {
  const { zoom, step, set } = useZoom(board.actions.toast);
  const pct = Math.round(zoom * 100);
  return (
    <>
      <h4>Appearance</h4>
      <div className="box">
        <div className="fg">
          <label htmlFor="ap-theme">Theme</label>
          <select id="ap-theme" className="in" style={{ width: "auto" }} value={board.theme} onChange={(e) => void board.actions.setTheme(e.target.value as Theme)}>
            <option value="system">Follow macOS</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
          <label htmlFor="ap-zoom">Zoom</label>
          <div className="inline">
            <button className="btn sm" type="button" aria-label="Zoom out" title={`Zoom out (${chordLabel("zoom.out")})`} disabled={zoom <= ZOOM_MIN} onClick={() => step(-1)}>−</button>
            <input
              id="ap-zoom"
              type="range"
              min={ZOOM_MIN * 100}
              max={ZOOM_MAX * 100}
              step={10}
              value={pct}
              aria-valuetext={`${pct}%`}
              onChange={(e) => set(parseInt(e.target.value, 10) / 100)}
            />
            <button className="btn sm" type="button" aria-label="Zoom in" title={`Zoom in (${chordLabel("zoom.in")})`} disabled={zoom >= ZOOM_MAX} onClick={() => step(1)}>+</button>
            <span className="mono" aria-label="Current zoom" style={{ minWidth: 44 }}>{pct}%</span>
            <button className="btn sm quiet" type="button" disabled={zoom === 1} onClick={() => step(0)}>Actual size ({chordLabel("zoom.reset")})</button>
          </div>
        </div>
      </div>
      <p className="hint">The zoom scales the whole window, terminals included, from 70% to 200%. {chordLabel("zoom.in")} and {chordLabel("zoom.out")} change it from anywhere, also while a terminal has the focus.</p>
    </>
  );
}
