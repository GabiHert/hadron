import { IntegrationsTab } from "./IntegrationsTab";
import { ImportTab } from "./ImportTab";
import { HistoryTab } from "./HistoryTab";
import { McpPanel } from "./McpPanel";
import { PromptsTab } from "./PromptsTab";
import { RulesTab } from "./RulesTab";
import { UpdatesCard } from "./UpdatesCard";
import { DiagnosticsTab } from "./DiagnosticsTab";
import { FeedbackTab } from "./FeedbackTab";
import { ModelsTab, useModels } from "./ModelsTab";
import { GeneralTab } from "./GeneralTab";
import { KeyboardTab } from "./KeyboardTab";
import { PlannerTab } from "./PlannerTab";
import { ProfileOptions } from "./ProfileGrant";
import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import * as api from "../api";
import type { Adoptable, Column, LaunchProfile } from "../api";
import { ROLES, SWATCHES, slug, tildePath } from "../lib";
import type { Board } from "../state/useBoard";
import { formatVersion, isDark } from "./Header";
import { Icon } from "./ui";
import { AdoptList, nickFrom } from "./AdoptList";

export function Scrim({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  return (
    <div className="scrim" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      {children}
    </div>
  );
}

export function ModalHead({ id, title, onClose }: { id: string; title: string; onClose: () => void }) {
  return (
    <div className="mh">
      <h3 id={id}>{title}</h3>
      <button className="ibtn" onClick={onClose} aria-label="Close"><Icon name="close" /></button>
    </div>
  );
}

export function NewWorkspace({ board, onClose }: { board: Board; onClose: () => void }) {
  const [name, setName] = useState("");
  const [root, setRoot] = useState("~/Documents/Projects/");
  const [color, setColor] = useState(SWATCHES[(board.visible.length) % SWATCHES.length]);
  const [tpl, setTpl] = useState<"planner" | "simple" | "copy">("planner");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    const w = await board.actions.createWorkspace({ name: name.trim(), root, color, template: tpl, ...(tpl === "copy" && board.ws ? { copy_from: board.ws.id } : {}) });
    setBusy(false);
    if (w) onClose();
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="nw-t">
        <ModalHead id="nw-t" title="New workspace" onClose={onClose} />
        <div className="mb">
          <label className="field"><span>Name</span><input id="nw-name" className="in" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Side project" /></label>
          <label className="field"><span>Root folder</span><input id="nw-root" className="in mono" value={root} onChange={(e) => setRoot(e.target.value)} /></label>
          <div className="field">
            <span>Colour</span>
            <div className="swatches">
              {SWATCHES.map((c) => <button key={c} className={"sw" + (color === c ? " on" : "")} style={{ background: c }} onClick={() => setColor(c)} aria-label={`Colour ${c}`} />)}
            </div>
          </div>
          <label className="field">
            <span>Columns</span>
            <select id="nw-tpl" className="in" value={tpl} onChange={(e) => setTpl(e.target.value as typeof tpl)}>
              <option value="planner">Planner default: Needs you, Working, Blocked, Done, Backlog</option>
              <option value="simple">Simple: To do, Doing, Done</option>
              {board.ws ? <option value="copy">Copy columns from {board.ws.name}</option> : null}
            </select>
          </label>
          <p className="hint" style={{ margin: 0 }}>You can rename columns and add rules later, or ask the planner to.</p>
        </div>
        <div className="mf">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn pri" disabled={!name.trim() || busy} onClick={() => void submit()}>Create workspace</button>
        </div>
      </div>
    </Scrim>
  );
}

/** "brief" becomes "Brief"; the label also says what the profile runs. */
export function profileLabel(p: LaunchProfile): string {
  const name = p.name.charAt(0).toUpperCase() + p.name.slice(1);
  const first = p.first_prompt.replace(/\{\{nick\}\}/g, "{nick}").replace(/\{\{key\}\}/g, "{key}").replace(/\{\{title\}\}/g, "{title}");
  return `${name}: ${p.command}${first ? `, then ${first}` : ", no first prompt"}`;
}

/** The one-line explanation under the picker: what Hadron will run and type. */
export function profileExplanation(p: LaunchProfile | undefined, nick: string, key: string): string {
  if (!p) return "claude --append-system-prompt <hadron session prompt>";
  const isClaude = p.command.split("/").pop() === "claude";
  const base = isClaude ? `${p.command} --mcp-config <hadron mcp.json> --append-system-prompt <hadron session prompt>` : p.command;
  const first = p.first_prompt
    .replace(/\{\{nick\}\}/g, nick || "{nick}")
    .replace(/\{\{key\}\}/g, key || nick || "{key}")
    .replace(/\{\{title\}\}/g, "{title}")
    .trim();
  return first ? `${base}  →  ${first}` : base;
}

export function NewSession({ board, onClose }: { board: Board; onClose: () => void }) {
  const { columns, ws } = board;
  const active = (columns.find((c) => c.role === "active") ?? columns[0])?.id ?? "";
  const [nickIn, setNick] = useState("");
  const [key, setKey] = useState("");
  const [col, setCol] = useState(active);
  const [profiles, setProfiles] = useState<LaunchProfile[]>([]);
  const [profile, setProfile] = useState("");
  const [busy, setBusy] = useState(false);
  const [cardPrompt, setCardPrompt] = useState("");
  const [model, setModel] = useState("");
  const [folder, setFolder] = useState("");
  const { info: modelInfo } = useModels(ws?.id ?? "", board.actions.toast);
  const nick = slug(nickIn);
  const [adopting, setAdopting] = useState(false);
  const [picked, setPicked] = useState<Adoptable | null>(null);
  const [adoptProfile, setAdoptProfile] = useState("");
  const adopt = async () => {
    if (!picked) return;
    setBusy(true);
    const out = await board.actions.run(() => api.session_adopt({ workspace_id: ws?.id, claude_session_id: picked.claude_session_id, nick, key: key.trim() || undefined, column_id: col || undefined, ...(adoptProfile ? { launch_profile: adoptProfile } : {}), ...(picked.cwd ? { cwd: picked.cwd } : {}) }));
    setBusy(false);
    if (!out) return;
    if (out.card) board.actions.select(out.card.id);
    board.actions.toast(out.warning);
    onClose();
  };
  useEffect(() => {
    let alive = true;
    api
      .launch_profile_list(ws ? { workspace_id: ws.id } : {})
      .then((list) => {
        if (!alive) return;
        setProfiles(list);
        // Brief first is the usual start; fall back to the first profile the workspace has.
        setProfile((cur) => cur || (list.find((p) => p.name === "brief") ?? list[0])?.name || "");
      })
      .catch((e) => board.actions.toast(e instanceof Error ? e.message : String(e)));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ws?.id]);
  const chosen = profiles.find((p) => p.name === profile);
  const submit = async () => {
    setBusy(true);
    const card = await board.actions.createCard({ nick, key: key.trim() || undefined, column_id: col || undefined, launch_profile: profile || undefined, ...(cardPrompt.trim() ? { prompt: cardPrompt } : {}), ...(model ? { model } : {}), ...(folder.trim() ? { cwd: folder.trim() } : {}) });
    setBusy(false);
    if (card) onClose();
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="ns-t">
        <ModalHead id="ns-t" title="New session" onClose={onClose} />
        {adopting ? (
        <>
        <div className="mb">
          <AdoptList workspaceId={ws?.id ?? ""} root={ws?.root ?? ""} picked={picked?.claude_session_id ?? ""} onError={board.actions.toast} onPick={(a) => { setPicked(a); setNick(nickFrom(a.first_user_message)); }} />
          {picked ? (
            <>
              {picked.cwd ? <p className="hint" aria-label="Adopted folder" style={{ margin: 0 }}>Runs in <span className="mono">{tildePath(picked.cwd)}</span></p> : null}
              <label className="field"><span>Nickname</span><input id="ad-nick" className="in" autoFocus value={nickIn} onChange={(e) => setNick(e.target.value)} placeholder="billing-fix" /></label>
              <label className="field"><span>Key (optional)</span><input id="ad-key" className="in mono" value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} placeholder="ACME-550" /></label>
              <label className="field"><span>Launch profile</span>
                <select id="ad-prof" className="in" aria-label="Adopt launch profile" value={adoptProfile} onChange={(e) => setAdoptProfile(e.target.value)}>
                  <ProfileOptions profiles={profiles} current={adoptProfile} />
                </select>
              </label>
              <label className="field"><span>Column</span>
                <select id="ad-col" className="in" value={col} onChange={(e) => setCol(e.target.value)}>{columns.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
              </label>
            </>
          ) : null}
        </div>
        <div className="mf">
          <button className="btn" onClick={() => { setAdopting(false); setPicked(null); }}>Back</button>
          <button className="btn pri" disabled={!picked || !nick || busy} onClick={() => void adopt()}>Adopt</button>
        </div>
        </>
        ) : (
        <>
        <div className="mb">
          <label className="field"><span>Nickname</span><input id="ns-nick" className="in" autoFocus value={nickIn} onChange={(e) => setNick(e.target.value)} placeholder="billing-fix" /></label>
          <label className="field"><span>Key (optional)</span><input id="ns-key" className="in mono" value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} placeholder="ACME-550" /></label>
          <label className="field"><span>Column</span>
            <select id="ns-col" className="in" value={col} onChange={(e) => setCol(e.target.value)}>{columns.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
          </label>
          <label className="field">
            <span>Launch profile <span className="hint" style={{ fontWeight: 400 }}>· a saved recipe for how the session starts: command, model, first prompt, env, MCP servers, worktree</span></span>
            <select id="ns-prof" className="in" value={profile} onChange={(e) => setProfile(e.target.value)}>
              {profiles.length === 0 ? <option value="">Plain claude, no first prompt</option> : null}
              {profiles.map((p) => <option key={p.id} value={p.name}>{profileLabel(p)}</option>)}
            </select>
          </label>
          <label className="field">
            <span>Folder (optional)</span>
            <input id="ns-cwd" className="in mono" aria-label="Session folder" value={folder} onChange={(e) => setFolder(e.target.value)} placeholder={ws?.root ?? ""} />
            <span className="hint">Where the session runs. Empty means the workspace root; ~ is expanded.</span>
          </label>
          <label className="field">
            <span>Model</span>
            <select id="ns-model" className="in" aria-label="Model" value={model} onChange={(e) => setModel(e.target.value)}>
              <option value="">Default</option>
              {(modelInfo?.models ?? []).map((m) => <option key={m.id} value={m.id}>{m.label || m.id}</option>)}
            </select>
          </label>
          <label className="field">
            <span>Extra instructions for this session</span>
            <textarea id="ns-prompt" className="in mono" rows={3} value={cardPrompt} onChange={(e) => setCardPrompt(e.target.value)} />
            <span className="hint">Added to the system prompt after the workspace and launch profile prompts.</span>
          </label>
          <div className="code" id="ns-explain" aria-label="What this profile runs">{profileExplanation(chosen, nick, key.trim())}</div>
          {nickIn && nick !== nickIn ? <p className="hint" style={{ margin: 0 }}>The nickname will be saved as <b>{nick}</b>.</p> : null}
        </div>
        <div className="mf">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn" aria-label="Bring in a session" onClick={() => { setAdopting(true); setNick(""); }}>Bring in a session</button>
          <button className="btn pri" disabled={!nick || busy} onClick={() => void submit()}>Start session</button>
        </div>
        </>
        )}
      </div>
    </Scrim>
  );
}

type SettingsCtx = { onWhatsNew?: () => void };
type SettingsTab = { id: string; label: string; render: (board: Board, ctx: SettingsCtx) => ReactNode };

/** Add a tab by adding an entry. */
export const SETTINGS_TABS: SettingsTab[] = [
  { id: "general", label: "General", render: (b) => <GeneralTab toast={b.actions.toast} /> },
  { id: "keyboard", label: "Keyboard", render: (b) => <KeyboardTab toast={b.actions.toast} /> },
  { id: "prompts", label: "Harness", render: (b) => <PromptsTab board={b} /> },
  { id: "columns", label: "Columns", render: (b) => <ColumnsTab board={b} /> },
  { id: "models", label: "Models", render: (b) => <ModelsTab board={b} /> },
  { id: "planner", label: "Planner", render: (b) => <PlannerTab board={b} /> },
  { id: "rules", label: "Rules", render: (b) => <RulesTab board={b} /> },
  { id: "integrations", label: "Integrations", render: (b) => <IntegrationsTab board={b} /> },
  { id: "import", label: "Import", render: (b) => <ImportTab board={b} /> },
  { id: "widgets", label: "Widgets", render: () => <WidgetsTab /> },
  { id: "diagnostics", label: "Diagnostics", render: (b) => <DiagnosticsTab toast={b.actions.toast} /> },
  { id: "feedback", label: "Feedback", render: (b) => <FeedbackTab toast={b.actions.toast} /> },
  { id: "history", label: "History", render: (b) => <HistoryTab board={b} /> },
  { id: "mcp", label: "MCP server", render: () => <McpPanel /> },
  { id: "updates", label: "Updates", render: (_b, ctx) => <UpdatesCard onWhatsNew={ctx.onWhatsNew} /> },
];

export function Settings({ board, onClose, tabs = SETTINGS_TABS, initialTab = "columns", onTab, onWhatsNew }: { board: Board; onClose: () => void; tabs?: SettingsTab[]; initialTab?: string; onTab?: (id: string) => void; onWhatsNew?: () => void }) {
  const [tab, setTab] = useState(initialTab);
  useEffect(() => onTab?.(tab), [tab, onTab]);
  const cur = tabs.find((t) => t.id === tab) ?? tabs[0];
  return (
    <Scrim onClose={onClose}>
      <div className="modal wide" role="dialog" aria-modal="true" aria-labelledby="st-t">
        <ModalHead id="st-t" title={`Settings${board.ws ? ` · ${board.ws.name}` : ""}${board.version ? ` · Hadron ${formatVersion(board.version)}` : ""}`} onClose={onClose} />
        <div className="settings">
          <nav className="snav" aria-label="Settings sections">
            {tabs.map((t) => <button key={t.id} className={tab === t.id ? "on" : ""} onClick={() => setTab(t.id)}>{t.label}</button>)}
          </nav>
          <div className="sbody">{cur.render(board, { onWhatsNew })}</div>
        </div>
      </div>
    </Scrim>
  );
}

/** The one switch for widgets: whether the HTML the planner and sessions post may run scripts. */
function WidgetsTab() {
  const [on, setOn] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    let alive = true;
    api.settings_get({ key: "widgets.scripts" }).then((s) => { if (alive) setOn(s["widgets.scripts"] === true); }).catch(() => undefined);
    return () => { alive = false; };
  }, []);
  const toggle = async (v: boolean) => {
    setErr("");
    try {
      await api.settings_update({ values: { "widgets.scripts": v } });
      setOn(v);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };
  return (
    <>
      <h4>Widgets</h4>
      <p className="sub">The planner and your sessions can post small HTML widgets in chats and pin them to the dashboard. They render in a sandboxed frame with no network, no cookies and no access to Hadron.</p>
      <div className="box">
        <div className="rrow">
          <button className={"switch" + (on ? " on" : "")} onClick={() => void toggle(!on)} aria-pressed={on} aria-label="Allow scripts in widgets" />
          <span style={{ flex: 1 }}><b style={{ fontWeight: 500 }}>Allow scripts in widgets</b><br /><span className="hint">Off by default. With it on, a widget can run its own JavaScript inside the frame, still with no network. Turn it on only if you trust what writes your widgets.</span></span>
        </div>
      </div>
      {err ? <p className="hint" role="alert">{err}</p> : null}
    </>
  );
}

function ColumnsTab({ board }: { board: Board }) {
  const { columns, cards, actions } = board;
  return (
    <>
      <h4>Columns</h4>
      <p className="sub">Names are yours. Rules move cards by column id and role, so renaming never breaks a rule.</p>
      <div className="scroll">
        <table className="table">
          <thead><tr><th>Name</th><th>Role</th><th>WIP</th><th>Cards</th><th></th></tr></thead>
          <tbody>
            {columns.map((c: Column, i: number) => (
              <tr key={c.id}>
                <td><input className="in" defaultValue={c.name} aria-label="Column name" id={`set-col-${c.id}`} onBlur={(e) => { const n = e.target.value.trim(); if (n && n !== c.name) void actions.renameColumn(c.id, n); }} /></td>
                <td>
                  <select className="in" value={c.role} aria-label="Role" onChange={(e) => void actions.updateColumn(c.id, { role: e.target.value as Column["role"] })}>
                    <option value="">No role</option>
                    {ROLES.map((r) => <option key={r[0]} value={r[0]}>{r[1]}</option>)}
                  </select>
                </td>
                <td><input className="in" type="number" min={0} style={{ width: 70 }} value={c.wip} aria-label="WIP limit" id={`set-wip-${c.id}`} onChange={(e) => void actions.updateColumn(c.id, { wip: Math.max(0, parseInt(e.target.value || "0", 10)) })} /></td>
                <td>{cards.filter((x) => x.column_id === c.id).length}</td>
                <td>
                  <div className="inline">
                    <button className="btn sm" onClick={() => void actions.moveColumn(c.id, -1)} disabled={i === 0} aria-label="Move up">↑</button>
                    <button className="btn sm" onClick={() => void actions.moveColumn(c.id, 1)} disabled={i === columns.length - 1} aria-label="Move down">↓</button>
                    <button className="btn sm danger" onClick={() => void actions.deleteColumn(c.id)} disabled={columns.length < 2}>Delete</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div><button className="btn" onClick={() => void actions.addColumn()}><Icon name="plus" size={13} />Add column</button></div>
    </>
  );
}

interface PaletteProps {
  board: Board;
  onClose: () => void;
  openModal: (m: "newws" | "newsession" | "settings" | "feedback" | "whatsnew") => void;
  setPlannerView: (v: "chat" | "terminal") => void;
}

export function Palette({ board, onClose, openModal, setPlannerView }: PaletteProps) {
  const { actions, columns: _c, cards } = board;
  void _c;
  const [q, setQ] = useState("");
  const [i, setI] = useState(0);
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => ref.current?.focus(), []);
  const items = [
    ...board.visible.map((w) => ({ label: `Switch to ${w.name}`, hint: "workspace", run: () => void actions.switchWorkspace(w.id) })),
    ...cards.map((c) => ({ label: `Open ${c.nick}`, hint: c.key || "card", run: () => actions.open(c.id) })),
    { label: "New session", hint: "⌘N", run: () => openModal("newsession") },
    { label: "New workspace", hint: "", run: () => openModal("newws") },
    { label: "Send feedback", hint: "", run: () => openModal("feedback") },
    { label: "What's new", hint: "", run: () => openModal("whatsnew") },
    { label: "Settings: columns", hint: "", run: () => openModal("settings") },
    { label: "Switch light or dark", hint: "", run: () => void actions.setTheme(isDark(board.theme) ? "light" : "dark") },
    { label: "Planner: chat", hint: "", run: () => setPlannerView("chat") },
    { label: "Planner: terminal", hint: "", run: () => setPlannerView("terminal") },
  ];
  const list = items.filter((x) => x.label.toLowerCase().includes(q.toLowerCase())).slice(0, 12);
  const go = (x?: (typeof items)[number]) => {
    onClose();
    x?.run();
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal pal" role="dialog" aria-modal="true" aria-label="Command palette">
        <input
          ref={ref}
          id="pal-q"
          placeholder="Jump to a card, workspace or command"
          value={q}
          onChange={(e) => { setQ(e.target.value); setI(0); }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") { e.preventDefault(); setI(Math.min(i + 1, list.length - 1)); }
            if (e.key === "ArrowUp") { e.preventDefault(); setI(Math.max(i - 1, 0)); }
            if (e.key === "Enter") go(list[i]);
          }}
        />
        <ul>
          {list.map((x, j) => (
            <li key={x.label}>
              <button className={j === i ? "on" : ""} onClick={() => go(x)}><span style={{ flex: 1 }}>{x.label}</span><span className="hint">{x.hint}</span></button>
            </li>
          ))}
          {!list.length ? <li className="hint" style={{ padding: 10 }}>Nothing matches.</li> : null}
        </ul>
      </div>
    </Scrim>
  );
}
