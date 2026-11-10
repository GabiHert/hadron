import { useRef, useState } from "react";
import type { CSSProperties } from "react";
import * as api from "../api";
import { ZOOM_MAX, ZOOM_MIN } from "../commands";
import { chordLabel } from "../keys";
import type { Board } from "../state/useBoard";
import { useZoom } from "../state/useZoom";
import { loadThemes, resolve, themeActions, useThemes } from "../theme";
import type { ThemeDef, TokenGroup } from "../theme";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

function readText(f: File): Promise<string> {
  if (typeof f.text === "function") return f.text();
  return new Promise((ok, fail) => {
    const r = new FileReader();
    r.onload = () => ok(String(r.result ?? ""));
    r.onerror = () => fail(r.error);
    r.readAsText(f);
  });
}
const TAGS = ["grey", "blue", "teal", "green", "lime", "amber", "orange", "red", "pink", "purple"];

/** Inline custom properties that paint a subtree in a theme, whatever the app shows. */
export function themeStyle(t: Pick<ThemeDef, "tokens">): CSSProperties {
  const s: Record<string, string> = {};
  for (const [k, v] of Object.entries(t.tokens)) s[`--${k}`] = v;
  return s as CSSProperties;
}

/** A small Hadron screen in one theme: a card with tags, a button and a terminal. */
export function ThemePreview({ theme, label }: { theme: Pick<ThemeDef, "tokens" | "name">; label?: string }) {
  return (
    <div className="tprev" style={themeStyle(theme)} aria-label={label ?? `Preview of ${theme.name}`} role="img">
      <div className="tp-top"><b>hadron</b><span>Board</span><span className="on">Dashboard</span></div>
      <div className="tp-body">
        <div className="tp-card">
          <div className="tp-k">ACME-508</div>
          <div className="tp-t">Retry webhook on 429</div>
          <div className="tp-chips">
            <span className="tp-pill" style={{ color: "var(--warn)", background: "var(--warnb)" }}>Needs you</span>
            <span className="tp-pill" style={{ color: "var(--ok)", background: "var(--okb)" }}>Merged</span>
            <span className="tp-pill" style={{ color: "var(--bad)", background: "var(--badb)" }}>Failed</span>
          </div>
        </div>
        <div className="tp-side">
          <div className="tp-bub">billing-fix passed 48 tests.</div>
          <span className="tp-btn">Approve</span>
        </div>
      </div>
      <div className="tp-tags">{TAGS.map((n) => <span key={n} className="tp-pill" style={{ color: `var(--tag-${n})`, background: `var(--tag-${n}-b)` }}>{n}</span>)}</div>
      <pre className="tp-term"><span style={{ color: "var(--tdim)" }}>~/acme $</span> go test ./billing{"\n"}<span style={{ color: "var(--tg)" }}>ok</span> acme/billing <span style={{ color: "var(--ty)" }}>0.41s</span> <span style={{ color: "var(--tr)" }}>-1</span> <span style={{ color: "var(--tb)" }}>●</span> <span style={{ color: "var(--tm)" }}>✻</span> <span style={{ color: "var(--tc)" }}>tax.go</span></pre>
    </div>
  );
}

interface Draft {
  id?: string;
  name: string;
  mode: "light" | "dark";
  tokens: Record<string, string>;
}

/** The theme editor: name, mode and every token, with a live preview. */
function ThemeEditor({ draft: start, groups, onDone, toast }: { draft: Draft; groups: TokenGroup[]; onDone: (saved?: ThemeDef) => void; toast: (t: string) => void }) {
  const [d, setD] = useState<Draft>(start);
  const [tryIt, setTryIt] = useState(false);
  const [confirmDel, setConfirmDel] = useState(false);
  const setTok = (k: string, v: string) => {
    const next = { ...d, tokens: { ...d.tokens, [k]: v } };
    setD(next);
    if (tryIt) themeActions.preview({ id: "draft", builtin: false, ...next });
  };
  const close = (saved?: ThemeDef) => {
    themeActions.preview(null);
    onDone(saved);
  };
  const save = async () => {
    try {
      const saved = await api.theme_save({ theme: { id: d.id, name: d.name, mode: d.mode, tokens: d.tokens } });
      toast(`Saved ${saved.name}`);
      close(saved);
    } catch (e) {
      toast(errText(e));
    }
  };
  const del = async () => {
    if (!d.id) return;
    try {
      await api.theme_delete({ id: d.id });
      await loadThemes();
      toast(`Deleted ${d.name}`);
      close();
    } catch (e) {
      toast(errText(e));
    }
  };
  return (
    <div className="box teditor" role="group" aria-label="Theme editor">
      <div className="boxh"><b>{d.id ? `Edit ${start.name}` : "New theme"}</b></div>
      <div className="te-grid">
        <div className="te-fields">
          <div className="fg">
            <label htmlFor="te-name">Name</label>
            <input id="te-name" className="in" value={d.name} maxLength={60} onChange={(e) => setD({ ...d, name: e.target.value })} />
            <label htmlFor="te-mode">Mode</label>
            <select id="te-mode" className="in" style={{ width: "auto" }} value={d.mode} onChange={(e) => setD({ ...d, mode: e.target.value as Draft["mode"] })}>
              <option value="light">Light</option>
              <option value="dark">Dark</option>
            </select>
          </div>
          {groups.map((g) => (
            <fieldset key={g.name} className="te-group">
              <legend className="cap">{g.name}</legend>
              <div className="te-toks">
                {g.tokens.map((k) => (
                  <label key={k} className="te-tok">
                    <input type="color" aria-label={`${k} colour`} value={d.tokens[k] ?? "#000000"} onChange={(e) => setTok(k, e.target.value)} />
                    <span className="mono">{k}</span>
                    <input className="in mono" aria-label={`${k} hex`} value={d.tokens[k] ?? ""} spellCheck={false}
                      onChange={(e) => { if (/^#[0-9a-fA-F]{6}$/.test(e.target.value)) setTok(k, e.target.value.toLowerCase()); else setD({ ...d, tokens: { ...d.tokens, [k]: e.target.value } }); }} />
                  </label>
                ))}
              </div>
            </fieldset>
          ))}
        </div>
        <div className="te-side">
          <ThemePreview theme={d} label="Live preview" />
          <label className="inline">
            <input type="checkbox" checked={tryIt} onChange={(e) => { setTryIt(e.target.checked); themeActions.preview(e.target.checked ? { id: "draft", builtin: false, ...d } : null); }} />
            <span>Try it on the whole app</span>
          </label>
        </div>
      </div>
      <div className="te-actions">
        <button className="btn pri" type="button" onClick={() => void save()} disabled={!d.name.trim()}>Save</button>
        <button className="btn" type="button" onClick={() => close()}>Cancel</button>
        <span className="spacer" />
        {d.id ? (
          confirmDel ? (
            <button className="btn danger" type="button" onClick={() => void del()}>Delete {start.name} for good</button>
          ) : (
            <button className="btn quiet" type="button" onClick={() => setConfirmDel(true)}>Delete theme</button>
          )
        ) : null}
      </div>
    </div>
  );
}

/** Settings → Appearance: the colour theme (fixed, or following macOS), custom themes, and the zoom. */
export function AppearanceTab({ board }: { board: Board }) {
  const { zoom, step, set } = useZoom(board.actions.toast);
  const pct = Math.round(zoom * 100);
  const toast = board.actions.toast;
  const { list, systemDark } = useThemes();
  const [hover, setHover] = useState<ThemeDef | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const file = useRef<HTMLInputElement>(null);
  const themes = list?.themes ?? [];
  const active = list?.active;
  const showing = list ? resolve(list, systemDark) : undefined;
  const system = active?.theme === "system";
  const lights = themes.filter((t) => t.mode === "light");
  const darks = themes.filter((t) => t.mode === "dark");
  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
    } catch (e) {
      toast(errText(e));
    }
  };
  const duplicate = (t: ThemeDef) => setDraft({ name: `${t.name} copy`, mode: t.mode, tokens: { ...t.tokens } });
  const onImport = async (f: File | undefined) => {
    if (!f) return;
    await run(async () => {
      const t = await api.theme_import({ json: await readText(f) });
      toast(`Imported ${t.name}`);
      await themeActions.pick(t);
    });
    if (file.current) file.current.value = "";
  };
  const exportTheme = (t: ThemeDef) => run(async () => toast(`Exported ${t.name} to ${(await api.theme_export({ id: t.id })).path}`));
  const badge = (t: ThemeDef) => {
    if (!active) return null;
    if (!system) return active.theme === t.id ? "In use" : null;
    if (active.light === t.id) return "Light";
    if (active.dark === t.id) return "Dark";
    return null;
  };
  const opts = (ts: ThemeDef[]) => ts.map((t) => <option key={t.id} value={t.id}>{t.name}</option>);

  return (
    <>
      <h4>Appearance</h4>
      <div className="box">
        <div className="fg">
          <span className="fgl">Theme</span>
          <label className="inline">
            <input id="ap-system" type="checkbox" aria-label="Follow macOS (light and dark)" checked={system} disabled={!list} onChange={(e) => void run(() => themeActions.setSystem(e.target.checked))} />
            <span>Follow macOS (light and dark)</span>
          </label>
          {system ? (
            <>
              <label htmlFor="ap-light">Light theme</label>
              <select id="ap-light" className="in" style={{ width: "auto" }} value={active?.light} onChange={(e) => void run(() => api.theme_set({ light: e.target.value }).then(() => undefined))}>
                <optgroup label="Light">{opts(lights)}</optgroup>
                <optgroup label="Dark">{opts(darks)}</optgroup>
              </select>
              <label htmlFor="ap-dark">Dark theme</label>
              <select id="ap-dark" className="in" style={{ width: "auto" }} value={active?.dark} onChange={(e) => void run(() => api.theme_set({ dark: e.target.value }).then(() => undefined))}>
                <optgroup label="Dark">{opts(darks)}</optgroup>
                <optgroup label="Light">{opts(lights)}</optgroup>
              </select>
            </>
          ) : (
            <>
              <label htmlFor="ap-theme">Use</label>
              <select id="ap-theme" className="in" style={{ width: "auto" }} value={active?.theme} onChange={(e) => void run(() => api.theme_set({ theme: e.target.value }).then(() => undefined))}>
                <optgroup label="Light">{opts(lights)}</optgroup>
                <optgroup label="Dark">{opts(darks)}</optgroup>
              </select>
            </>
          )}
          <label htmlFor="ap-zoom">Zoom</label>
          <div className="inline">
            <button className="btn sm" type="button" aria-label="Zoom out" title={`Zoom out (${chordLabel("zoom.out")})`} disabled={zoom <= ZOOM_MIN} onClick={() => step(-1)}>−</button>
            <input id="ap-zoom" type="range" min={ZOOM_MIN * 100} max={ZOOM_MAX * 100} step={10} value={pct} aria-valuetext={`${pct}%`} onChange={(e) => set(parseInt(e.target.value, 10) / 100)} />
            <button className="btn sm" type="button" aria-label="Zoom in" title={`Zoom in (${chordLabel("zoom.in")})`} disabled={zoom >= ZOOM_MAX} onClick={() => step(1)}>+</button>
            <span className="mono" aria-label="Current zoom" style={{ minWidth: 44 }}>{pct}%</span>
            <button className="btn sm quiet" type="button" disabled={zoom === 1} onClick={() => step(0)}>Actual size ({chordLabel("zoom.reset")})</button>
          </div>
        </div>
      </div>
      <p className="hint">
        {system ? "Click a theme to use it for its mode: a light theme while macOS is light, a dark one while it is dark." : "Click a theme to use it."} The zoom scales the whole window, terminals included, from 70% to 200%.
      </p>

      {draft ? (
        <ThemeEditor key={draft.id ?? "new"} draft={draft} groups={list?.groups ?? []} toast={toast}
          onDone={(saved) => { setDraft(null); if (saved) void run(() => themeActions.pick(saved)); }} />
      ) : (
        <>
          <div className="inline" style={{ margin: "14px 0 8px" }}>
            <b style={{ flex: 1 }}>Themes</b>
            <button className="btn sm" type="button" disabled={!showing} onClick={() => showing && duplicate(showing)}>New theme</button>
            <button className="btn sm" type="button" onClick={() => file.current?.click()}>Import VS Code theme…</button>
            <input ref={file} type="file" accept=".json,.jsonc,application/json" aria-label="VS Code theme file" hidden onChange={(e) => void onImport(e.target.files?.[0])} />
          </div>
          <div className="tpick">
            <div className="tgrid" role="list" aria-label="Themes">
              {themes.map((t) => {
                const b = badge(t);
                return (
                  <div key={t.id} role="listitem" className={"tsw" + (b ? " on" : "")} onMouseEnter={() => setHover(t)} onMouseLeave={() => setHover(null)}>
                    <button type="button" className="tsw-pick" aria-pressed={!!b} aria-label={`Use ${t.name}`} onClick={() => void run(() => themeActions.pick(t))} onFocus={() => setHover(t)}>
                      <span className="tsw-chip" style={themeStyle(t)}>
                        <i style={{ background: "var(--side)" }} /><i style={{ background: "var(--card)" }} /><i style={{ background: "var(--acc)" }} /><i style={{ background: "var(--ok)" }} /><i style={{ background: "var(--warn)" }} /><i style={{ background: "var(--bad)" }} />
                      </span>
                      <span className="tsw-name">{t.name}</span>
                      <span className="tsw-meta">{t.mode}{t.builtin ? "" : " · custom"}{b ? <b> · {b}</b> : null}</span>
                    </button>
                    <div className="tsw-acts">
                      <button type="button" className="btn sm quiet" aria-label={`Duplicate ${t.name}`} onClick={() => duplicate(t)}>Duplicate</button>
                      {t.builtin ? null : <button type="button" className="btn sm quiet" aria-label={`Edit ${t.name}`} onClick={() => setDraft({ id: t.id, name: t.name, mode: t.mode, tokens: { ...t.tokens } })}>Edit</button>}
                      <button type="button" className="btn sm quiet" aria-label={`Export ${t.name}`} onClick={() => void exportTheme(t)}>Export</button>
                    </div>
                  </div>
                );
              })}
            </div>
            {hover ?? showing ? <ThemePreview theme={(hover ?? showing)!} /> : null}
          </div>
          <p className="hint">Import reads a VS Code colour theme (.json, comments allowed): the editor, button and terminal colours carry over, and Hadron derives the tag colours. Export writes a file to Downloads that Import reads back.</p>
        </>
      )}
    </>
  );
}
