// An in-memory stand-in for api.ts, for component tests. Every function is a vi.fn that behaves
// like the backend enough for the UI to react (rename, move, delete...), so tests can assert both
// the calls made and what the user then sees.
import { vi } from "vitest";
import type { Feedback, BlockView, Card, Column, DryRunItem, FieldEntry, FilterSpec, PromptInfo, PromptKind, PromptLayer, Rule, RuleLogEntry, View, LaunchProfile, McpCall, McpInfo, Message, Question, Role, SessionStatus, Workspace, ScriptView, ScriptLog, Link, RemoteSession } from "../api";

export interface Fixture {
  workspaces: Workspace[];
  columns: Record<string, Column[]>;
  cards: Record<string, Card[]>;
  settings: Record<string, unknown>;
  version: string;
}

const ts = "2026-11-08T12:00:00Z";
export const ws = (id: string, name: string, over: Partial<Workspace> = {}): Workspace => ({
  id, created_at: ts, updated_at: ts, name, color: "#24408e", root: "/tmp/" + id, position: 0, archived: false,
  integrations: [], filter_bar: [], ui: {}, ...over,
});
export const col = (id: string, wsId: string, name: string, role: Role, position: number, over: Partial<Column> = {}): Column => ({
  id, workspace_id: wsId, name, role, position, wip: 0, fold: false, color: "", ...over,
});
export const card = (id: string, wsId: string, colId: string, nick: string, position: number, over: Partial<Card> = {}): Card => ({
  id, workspace_id: wsId, column_id: colId, position, nick, key: "", title: "", assignee: "You", stage: "", liveness: "idle",
  return_column_id: "", launch_profile: "", prompt: "", fields: {}, archived: false, ...over,
});

export function defaultFixture(): Fixture {
  return {
    version: "v0.0.1-test",
    settings: { active_workspace: "w1" },
    workspaces: [ws("w1", "Acme", { position: 0 }), ws("w2", "Work", { position: 1, color: "#2f7a4b" })],
    columns: {
      w1: [
        col("needs", "w1", "Needs you", "needs-you", 0),
        col("working", "w1", "Working", "active", 1, { wip: 4 }),
        col("done", "w1", "Done", "done", 2),
      ],
      w2: [col("inbox", "w2", "Inbox", "needs-you", 0), col("doing", "w2", "In progress", "active", 1)],
    },
    cards: {
      w1: [
        card("c1", "w1", "needs", "search-console", 0, { key: "ACME-508", liveness: "waiting" }),
        card("c2", "w1", "working", "api-timeouts", 0, { key: "ACME-541", liveness: "working" }),
        card("c3", "w1", "working", "dedupe-records", 1),
        card("c4", "w1", "working", "stale-warning", 2, { assignee: "Ana" }),
      ],
      w2: [card("c5", "w2", "inbox", "invoicing", 0)],
    },
  };
}

export const message = (over: Partial<Message> = {}): Message => ({
  id: "m1", created_at: new Date().toISOString(), updated_at: ts, workspace_id: "w1", channel: "planner", author: "planner", kind: "text",
  report_kind: "", text: "", widget_title: "", widget_html: "", delivered: true, reason: "", question_id: "", ...over,
});
export const question = (over: Partial<Question> = {}): Question => ({
  id: "q1", created_at: ts, updated_at: ts, workspace_id: "w1", card_id: "c1", seq: 1, kind: "decision", text: "Round invoices up or to nearest?", context: "",
  options: [{ label: "Nearest", description: "recommended" }, { label: "Up", description: "" }], multi: false, approval: null, status: "open", answer: null, answered_at: null, ...over,
});

export const idleUpdate = (over: Record<string, unknown> = {}) => ({
  version: "v0.0.1-test", repo: "GabiHert/hadron", latest: null, upToDate: false, hasToken: false, state: "idle", message: "",
  lines: [] as string[], seq: 0, installedApp: "/Applications/Hadron.app", ...over,
});
export const profile = (name: string, firstPrompt: string): LaunchProfile => ({
  id: "lp-" + name, workspace_id: "w1", name, command: "claude", args: [], first_prompt: firstPrompt, prompt: "", env: {}, worktree: false,
});
const defaultProfiles = () => [profile("brief", "/brief {{nick}}"), profile("orchestrate", "/orchestrate {{key}}"), profile("plain", "")];

/** A running session's status, as session_status and planner_get report it. */
export const running = (sessionId: string, over: Partial<SessionStatus> = {}): SessionStatus => ({
  workspace_id: "w1", card_id: "", nick: "", running: true, liveness: "idle", cols: 120, rows: 36, error: "",
  session: {
    id: sessionId, workspace_id: "w1", card_id: "", kind: "card", command: "claude", cwd: "/tmp/w1", pid: 4242, status: "running",
    exit_code: 0, claude_session_id: "", started_at: ts, ended_at: null,
  },
  ...over,
});

export const fieldEntry = (path: string, label: string, over: Partial<FieldEntry> = {}): FieldEntry => ({
  path, label, type: "text", filterable: true, editable: false, card_chip: false, source: "native", ...over,
});
export const defaultSchema = (): FieldEntry[] => [
  fieldEntry("card.column", "Column", { editable: true }),
  fieldEntry("card.assignee", "Assignee", { type: "user", editable: true }),
  fieldEntry("card.liveness", "Liveness", { type: "enum", options: ["working", "idle", "waiting", "ended"] }),
  fieldEntry("card.key", "Key", { editable: true }),
  fieldEntry("card.nick", "Nickname", { editable: true }),
  fieldEntry("card.stage", "Stage", { editable: true }),
  fieldEntry("jira.status", "Status", { type: "enum", options: ["To do", "In progress", "Finished"], editable: true, source: "jira" }),
];
const defaultPrompts = (): Record<PromptKind, PromptInfo> => ({
  planner: { kind: "planner", template: "", default: "", core: "Core harness for {{workspace.name}}", core_version: 3, custom: false, variables: ["workspace.name", "columns", "card.nick"], unknown: [] },
  session: { kind: "session", template: "", default: "", core: "Core harness for {{workspace.name}}", core_version: 3, custom: false, variables: ["workspace.name", "columns", "card.nick"], unknown: [] },
  handoff: { kind: "handoff", template: "Continue {{card.nick}} in {{workspace.name}}.", default: "Continue {{card.nick}} in {{workspace.name}}.", core: "", core_version: 3, custom: false, variables: ["card.nick", "workspace.name"], unknown: [] },
  handoff_planner: { kind: "handoff_planner", template: "Continue as the planner of {{workspace.name}}.", default: "Continue as the planner of {{workspace.name}}.", core: "", core_version: 3, custom: false, variables: ["workspace.name"], unknown: [] },
});

export const dashBlock = (id: string, over: Partial<BlockView> = {}): BlockView => ({
  id, created_at: ts, updated_at: ts, workspace_id: "w1", position: 0, type: "stats", title: "Block " + id, span: 1, source: {}, html: "", schedule: "", text: "", text_at: null, data: null, ...over,
});
const defaultDash = (): BlockView[] => [
  dashBlock("b1", { position: 0, type: "stats", title: "Cards by column", span: 2, source: { query: "cards.count", args: { by: "column" } },
    data: { query: "cards.count", shape: "groups", groups: [{ key: "needs", label: "Needs you", count: 1 }, { key: "working", label: "Working", count: 3 }], rows: [] } }),
  dashBlock("b2", { position: 1, type: "table", title: "Waiting on you", source: { query: "questions.open" },
    data: { query: "questions.open", shape: "rows", groups: [], rows: [] } }),
  dashBlock("b3", { position: 2, type: "planner", title: "Planner's summary", span: 2 }),
];

export const state: {
  dash: BlockView[];
  fx: Fixture;
  schema: FieldEntry[];
  views: View[];
  rules: Rule[];
  ruleLog: RuleLogEntry[];
  dryRun: DryRunItem[];
  prompts: Record<PromptKind, PromptInfo>;
  /** what filter_eval answers; null means "every card matching the vals and q" */
  filterError: string;
  update: ReturnType<typeof idleUpdate>;
  profiles: LaunchProfile[];
  /** session_status by card id; planner_get reads "planner". A card with no entry has never run. */
  sessions: Record<string, SessionStatus>;
  mcp: McpInfo;
  mcpCalls: McpCall[];
  /** every message, any channel */
  messages: Message[];
  questions: Question[];
  chips: Record<string, { path: string; label: string; value: unknown }[]>;
  integrations: ScriptView[];
  integrationLogs: ScriptLog[];
  links: Link[];
  remoteSessions: RemoteSession[];
} = { dash: defaultDash(), fx: defaultFixture(), schema: defaultSchema(), views: [], rules: [], ruleLog: [], dryRun: [], prompts: defaultPrompts(), filterError: "", update: idleUpdate(), profiles: defaultProfiles(), sessions: {},
  mcp: { running: true, url: "http://127.0.0.1:47820/mcp", port: 47820, token: "tok-0123456789abcdef", tool_count: 61, connect_command: "" },
  mcpCalls: [],
  messages: [],
  questions: [],
  chips: {},
  integrations: [],
  integrationLogs: [],
  links: [],
  remoteSessions: [],
};
export const reset = (fx: Fixture = defaultFixture()) => {
  state.update = idleUpdate();
  state.dash = defaultDash();
  state.schema = defaultSchema();
  state.views = [];
  state.rules = [];
  state.ruleLog = [];
  state.dryRun = [];
  state.prompts = defaultPrompts();
  state.filterError = "";
  state.mcpCalls = [];
  state.messages = [];
  state.questions = [];
  state.chips = {};
  state.integrations = [];
  state.integrationLogs = [];
  state.links = [];
  state.remoteSessions = [];
  state.profiles = defaultProfiles();
  state.sessions = { planner: running("sess-planner", { liveness: "working" }) };
  state.fx = fx;
  for (const f of Object.values(fns)) f.mockClear();
};

const allCols = () => Object.values(state.fx.columns).flat();
const allCards = () => Object.values(state.fx.cards).flat();
const clone = <T,>(v: T): T => structuredClone(v);

const fns = {
  app_status: vi.fn(async () => ({ version: state.fx.version, home: "/tmp", actions: 25, schema_version: 1 })),
  settings_get: vi.fn(async () => clone(state.fx.settings)),
  settings_update: vi.fn(async (a: { values: Record<string, unknown> }) => {
    Object.assign(state.fx.settings, a.values);
    return clone(state.fx.settings);
  }),
  workspace_list: vi.fn(async () => clone(state.fx.workspaces)),
  workspace_get: vi.fn(async (a: { workspace_id: string }) => clone(state.fx.workspaces.find((w) => w.id === a.workspace_id)!)),
  workspace_create: vi.fn(async (a: { name: string; template?: string }) => {
    const w = ws("new-" + a.name, a.name, { position: state.fx.workspaces.length });
    state.fx.workspaces.push(w);
    state.fx.columns[w.id] = [col("n1-" + w.id, w.id, "To do", "backlog", 0)];
    state.fx.cards[w.id] = [];
    return clone(w);
  }),
  workspace_update: vi.fn(async () => ({}) as Workspace),
  workspace_archive: vi.fn(async () => ({}) as Workspace),
  workspace_delete: vi.fn(async () => ({}) as Workspace),
  workspace_switch: vi.fn(async (a: { workspace_id: string }) => {
    state.fx.settings.active_workspace = a.workspace_id;
    return clone(state.fx.workspaces.find((w) => w.id === a.workspace_id)!);
  }),
  column_list: vi.fn(async (a: { workspace_id?: string } = {}) => clone([...(state.fx.columns[a.workspace_id ?? "w1"] ?? [])].sort((x, y) => x.position - y.position))),
  column_create: vi.fn(async (a: { workspace_id?: string; name: string; role?: Role }) => {
    const wsId = a.workspace_id ?? "w1";
    const c = col("new-col", wsId, a.name, a.role ?? "", state.fx.columns[wsId].length);
    state.fx.columns[wsId].push(c);
    return clone(c);
  }),
  column_update: vi.fn(async (a: { column_id: string; name?: string; role?: Role; wip?: number; fold?: boolean }) => {
    const c = allCols().find((x) => x.id === a.column_id)!;
    const { column_id: _id, ...patch } = a;
    void _id;
    Object.assign(c, patch);
    return clone(c);
  }),
  column_reorder: vi.fn(async (a: { workspace_id?: string; column_ids: string[] }) => {
    const wsId = a.workspace_id ?? "w1";
    state.fx.columns[wsId] = a.column_ids.map((id, i) => ({ ...state.fx.columns[wsId].find((c) => c.id === id)!, position: i }));
    return clone(state.fx.columns[wsId]);
  }),
  column_delete: vi.fn(async (a: { column_id: string }) => {
    const c = allCols().find((x) => x.id === a.column_id)!;
    const list = state.fx.columns[c.workspace_id];
    const target = list.find((x) => x.id !== c.id)!;
    const moved = state.fx.cards[c.workspace_id].filter((k) => k.column_id === c.id);
    moved.forEach((k) => (k.column_id = target.id));
    state.fx.columns[c.workspace_id] = list.filter((x) => x.id !== c.id);
    return { deleted: clone(c), moved_to: clone(target), card_count: moved.length };
  }),
  card_list: vi.fn(async (a: { workspace_id?: string } = {}) => clone((state.fx.cards[a.workspace_id ?? "w1"] ?? []).filter((c) => !c.archived))),
  card_get: vi.fn(async (a: { card_id: string }) => clone(allCards().find((c) => c.id === a.card_id)!)),
  card_create: vi.fn(async (a: { workspace_id?: string; nick: string; key?: string; column_id?: string; launch_profile?: string; prompt?: string }) => {
    const wsId = a.workspace_id ?? "w1";
    const c = card("new-card", wsId, a.column_id ?? state.fx.columns[wsId][0].id, a.nick, 99, { key: a.key ?? "", launch_profile: a.launch_profile ?? "", prompt: a.prompt ?? "" });
    state.fx.cards[wsId].push(c);
    return clone(c);
  }),
  card_update: vi.fn(async (a: { card_id: string } & Partial<Card>) => {
    const c = allCards().find((x) => x.id === a.card_id)!;
    const { card_id: _id, ...patch } = a;
    void _id;
    Object.assign(c, patch);
    return clone(c);
  }),
  card_move: vi.fn(async (a: { card_id: string; column_id: string; position?: number }) => {
    const c = allCards().find((x) => x.id === a.card_id)!;
    const list = state.fx.cards[c.workspace_id];
    const rest = list.filter((x) => x.column_id === a.column_id && x.id !== c.id).sort((x, y) => x.position - y.position);
    rest.splice(a.position ?? rest.length, 0, c);
    c.column_id = a.column_id;
    rest.forEach((x, i) => (x.position = i));
    return clone(c);
  }),
  card_archive: vi.fn(async (a: { card_id: string }) => {
    const c = allCards().find((x) => x.id === a.card_id)!;
    c.archived = true;
    return clone(c);
  }),
  card_delete: vi.fn(async (a: { card_id: string }) => {
    const c = allCards().find((x) => x.id === a.card_id)!;
    state.fx.cards[c.workspace_id] = state.fx.cards[c.workspace_id].filter((x) => x.id !== c.id);
    return clone(c);
  }),
  ui_panel_collapse: vi.fn(async (a: { workspace_id?: string; panel: "planner" | "card"; collapsed: boolean }) => {
    const w = state.fx.workspaces.find((x) => x.id === (a.workspace_id ?? "w1"))!;
    w.ui = { ...w.ui, collapsed: { ...w.ui.collapsed, [a.panel]: a.collapsed } };
    if (a.collapsed && w.ui.full === a.panel) delete w.ui.full;
    return clone(w);
  }),
  ui_fullscreen: vi.fn(async (a: { workspace_id?: string; panel?: "planner" | "card" | "" }) => {
    const w = state.fx.workspaces.find((x) => x.id === (a.workspace_id ?? "w1"))!;
    if (a.panel) w.ui = { ...w.ui, full: a.panel };
    else delete w.ui.full;
    return clone(w);
  }),
  ui_main_view: vi.fn(async (a: { workspace_id?: string; view: "board" | "dashboard" }) => {
    const w = state.fx.workspaces.find((x) => x.id === (a.workspace_id ?? "w1"))!;
    w.ui = { ...w.ui, main_view: a.view };
    return clone(w);
  }),
  launch_profile_list: vi.fn(async () => clone(state.profiles)),
  planner_get: vi.fn(async () => clone(state.sessions.planner ?? { ...running("", { liveness: "none", running: false }), session: null })),
  planner_start: vi.fn(async () => clone(state.sessions.planner)),
  planner_restart: vi.fn(async () => clone(state.sessions.planner)),
  session_status: vi.fn(async (a: { card: string }) => clone(state.sessions[a.card] ?? { ...running("", { liveness: "none", running: false, card_id: a.card }), session: null })),
  session_start: vi.fn(async (a: { card: string }) => clone(state.sessions[a.card] ?? running("sess-" + a.card))),
  session_restart: vi.fn(async (a: { card: string }) => clone(state.sessions[a.card] ?? running("sess-" + a.card))),
  chat_read: vi.fn(async (a: { channel: string }) => clone(state.messages.filter((m) => m.channel === a.channel))),
  chat_send: vi.fn(async (a: { workspace_id?: string; channel: string; text: string }) => {
    const m = message({ id: "m" + (state.messages.length + 1), workspace_id: a.workspace_id ?? "w1", channel: a.channel, author: "you", text: a.text });
    state.messages.push(m);
    return clone(m);
  }),
  question_list: vi.fn(async (a: { workspace_id?: string; card?: string; status?: string }) =>
    clone(state.questions.filter((q) => (!a.workspace_id || q.workspace_id === a.workspace_id) && (!a.card || q.card_id === a.card) && (!a.status || q.status === a.status))),
  ),
  question_answer: vi.fn(async (a: { question_id: string; selections?: string[]; notes?: string }) => {
    const q = state.questions.find((x) => x.id === a.question_id)!;
    q.status = "answered";
    q.answer = { selections: a.selections ?? [], notes: a.notes ?? "", via: "you" };
    return clone(q);
  }),
  field_schema_get: vi.fn(async () => ({ fields: clone(state.schema), namespaces: ["card", "custom"] })),
  filter_eval: vi.fn(async (a: { workspace_id?: string; expr?: string; vals?: Record<string, string>; q?: string }) => {
    if (state.filterError && a.expr) throw new Error(state.filterError);
    const cards = state.fx.cards[a.workspace_id ?? "w1"] ?? [];
    const ids = cards.filter((c) => (!a.vals || Object.entries(a.vals).every(([p, v]) => !v || (p === "card.assignee" ? c.assignee === v : String(c.fields[p.split(".")[1]] ?? "") === v))) && (!a.q || c.nick.includes(a.q))).map((c) => c.id);
    return { card_ids: ids };
  }),
  filter_bar_set: vi.fn(async (a: { workspace_id?: string; fields: string[] }) => {
    const w = state.fx.workspaces.find((x) => x.id === (a.workspace_id ?? "w1"))!;
    w.filter_bar = a.fields;
    return clone(w);
  }),
  view_list: vi.fn(async () => clone(state.views)),
  view_save: vi.fn(async (a: { workspace_id?: string; name: string; filters: FilterSpec }) => {
    const v: View = { id: "v" + (state.views.length + 1), created_at: ts, updated_at: ts, workspace_id: a.workspace_id ?? "w1", name: a.name, filters: a.filters, position: state.views.length };
    state.views.push(v);
    return clone(v);
  }),
  view_delete: vi.fn(async (a: { view_id: string }) => {
    const v = state.views.find((x) => x.id === a.view_id)!;
    state.views = state.views.filter((x) => x.id !== a.view_id);
    return clone(v);
  }),
  rule_list: vi.fn(async () => clone(state.rules)),
  rule_create: vi.fn(async (a: { workspace_id?: string; name: string; trigger: Rule["trigger"]; condition?: string; actions: Rule["actions"] }) => {
    const r: Rule = { id: "r" + (state.rules.length + 1), created_at: ts, updated_at: ts, workspace_id: a.workspace_id ?? "w1", position: state.rules.length, name: a.name, enabled: true, trigger: a.trigger, condition: a.condition ?? "", actions: a.actions, builtin: false };
    state.rules.push(r);
    return clone(r);
  }),
  rule_update: vi.fn(async (a: { rule_id: string; enabled?: boolean }) => {
    const r = state.rules.find((x) => x.id === a.rule_id)!;
    if (a.enabled !== undefined) r.enabled = a.enabled;
    return clone(r);
  }),
  rule_reorder: vi.fn(async (a: { rule_ids: string[] }) => {
    state.rules = a.rule_ids.map((id, i) => ({ ...state.rules.find((r) => r.id === id)!, position: i }));
    return clone(state.rules);
  }),
  rule_delete: vi.fn(async (a: { rule_id: string }) => {
    const r = state.rules.find((x) => x.id === a.rule_id)!;
    state.rules = state.rules.filter((x) => x.id !== a.rule_id);
    return clone(r);
  }),
  rule_dry_run: vi.fn(async () => clone(state.dryRun)),
  rule_log: vi.fn(async () => clone(state.ruleLog)),
  prompt_get: vi.fn(async (a: { kind: PromptKind }) => clone(state.prompts[a.kind])),
  prompt_set: vi.fn(async (a: { kind: PromptKind; template: string }) => {
    const unknown = [...a.template.matchAll(/\{\{([a-z.]+)\}\}/g)].map((m) => m[1]).filter((v) => !state.prompts[a.kind].variables.includes(v));
    state.prompts[a.kind] = { ...state.prompts[a.kind], template: a.template, custom: true, unknown, warning: unknown.length ? "unknown variable(s) " + unknown.join(", ") : undefined };
    return clone(state.prompts[a.kind]);
  }),
  prompt_reset: vi.fn(async (a: { kind: PromptKind }) => {
    const p = state.prompts[a.kind];
    state.prompts[a.kind] = { ...p, template: p.default, custom: false, unknown: [], warning: undefined };
    return clone(state.prompts[a.kind]);
  }),
  prompt_render: vi.fn(async (a: { kind: PromptKind; template?: string; card?: string }) => {
    const p = state.prompts[a.kind];
    const fill = (t: string) => t.replaceAll("{{workspace.name}}", "Acme").replaceAll("{{card.nick}}", a.card ?? "example-card");
    const tpl = fill(a.template ?? p.template);
    return { text: p.core ? fill(p.core) + "\n\n## Workspace instructions\n\n" + tpl : tpl, unknown: [] as string[] };
  }),
  prompt_layers_get: vi.fn(async (a: { kind: PromptKind; card?: string }) => {
    const p = state.prompts[a.kind];
    const c = a.card ? allCards().find((x) => x.id === a.card || x.nick === a.card) : undefined;
    const fill = (t: string) => t.replaceAll("{{workspace.name}}", "Acme").replaceAll("{{card.nick}}", c?.nick ?? "example-card");
    const layers: PromptLayer[] = [];
    if (p.core) layers.push({ name: "core", title: "Core harness", editable: false, template: p.core, rendered: fill(p.core) });
    layers.push({ name: "workspace", title: "Workspace prompt", editable: true, template: p.template, rendered: fill(p.template) });
    if (c?.prompt) layers.push({ name: "card", title: "Card prompt", editable: true, template: c.prompt, rendered: c.prompt });
    return { kind: a.kind, core_version: p.core_version, layers, text: layers.map((l) => l.rendered ?? "").filter(Boolean).join("\n\n") };
  }),
  hadron_guide: vi.fn(async () => ({ topic: "index", text: "Guide", topics: ["index"], core_version: 3 })),
  mcp_info: vi.fn(async () => clone(state.mcp)),
  mcp_calls: vi.fn(async () => clone(state.mcpCalls)),
  card_chips: vi.fn(async () => clone(state.chips)),
  integration_list: vi.fn(async () => clone(state.integrations)),
  integration_templates: vi.fn(async () => [{ name: "rest", description: "A REST API" }]),
  integration_logs: vi.fn(async () => clone(state.integrationLogs)),
  integration_scaffold: vi.fn(async () => ({})),
  integration_approve: vi.fn(async () => ({})),
  integration_disable: vi.fn(async () => ({})),
  integration_remove: vi.fn(async () => ({})),
  integration_test: vi.fn(async () => ({ ok: true, passed: 0, failed: 0, cases: [], ran_at: "" })),
  integration_read_file: vi.fn(async (a: { path: string }) => ({ path: a.path, content: "// " + a.path })),
  integration_config_set: vi.fn(async () => ({})),
  integration_secret_set: vi.fn(async () => ({})),
  integration_secret_clear: vi.fn(async () => ({})),
  integration_use: vi.fn(async () => ({})),
  link_list: vi.fn(async () => clone(state.links)),
  link_search: vi.fn(async () => ({ items: [] as { item_id: string; title: string; url: string; fields: Record<string, unknown> }[] })),
  link_add: vi.fn(async () => ({})),
  link_remove: vi.fn(async () => ({})),
  link_refresh: vi.fn(async () => ({ links: [], changed: [] })),
  link_action_run: vi.fn(async () => ({ output: null, logs: [], duration_ms: 0 })),
  remote_session_list: vi.fn(async () => clone(state.remoteSessions)),
  remote_session_start: vi.fn(async () => ({})),
  remote_session_stop: vi.fn(async () => ({})),
  card_field_set: vi.fn(async (a: { path: string; value: unknown }) => ({ path: a.path, value: a.value })),
  dash_get: vi.fn(async () => ({ blocks: clone(state.dash), queries: [] })),
  dash_set: vi.fn(async () => [] as never[]),
  dash_block_add: vi.fn(async (a: { type: BlockView["type"]; title: string; span?: 1 | 2; source?: BlockView["source"] }) => {
    const b = dashBlock("nb" + (state.dash.length + 1), { type: a.type, title: a.title, span: a.span ?? 1, source: a.source ?? {}, position: state.dash.length });
    state.dash.push(b);
    return clone(b);
  }),
  dash_block_update: vi.fn(async (a: { block_id: string; span?: 1 | 2 }) => {
    const b = state.dash.find((x) => x.id === a.block_id)!;
    if (a.span) b.span = a.span;
    return clone(b);
  }),
  dash_block_move: vi.fn(async (a: { block_id: string; position: number }) => {
    const i = state.dash.findIndex((x) => x.id === a.block_id);
    const [b] = state.dash.splice(i, 1);
    state.dash.splice(a.position, 0, b);
    return clone(state.dash);
  }),
  dash_block_remove: vi.fn(async (a: { block_id: string }) => {
    const b = state.dash.find((x) => x.id === a.block_id)!;
    state.dash = state.dash.filter((x) => x.id !== a.block_id);
    return clone(b);
  }),
  dash_block_refresh: vi.fn(async (a: { block_id: string }) => clone(state.dash.find((x) => x.id === a.block_id)!)),
  dash_query_run: vi.fn(async (a: { query: string }) => ({ query: a.query, shape: "rows" as const, groups: [], rows: [] })),
  widget_pin: vi.fn(async (a: { message_id: string }) => {
    const b = dashBlock("pin-" + a.message_id, { type: "custom", title: "Pinned", source: { from: a.message_id }, position: state.dash.length });
    state.dash.push(b);
    return clone(b);
  }),
  config_history: vi.fn(async (): Promise<unknown[]> => []),
  config_diff: vi.fn(async (): Promise<{ change: unknown; diff: { field: string; before: unknown; after: unknown }[] }> => ({ change: {}, diff: [] })),
  config_revert: vi.fn(async () => ({ reverted: {}, summary: "restored the column to its earlier state" })),
  session_adoptable_list: vi.fn(async (): Promise<unknown[]> => []),
  session_adopt: vi.fn(async (): Promise<unknown> => ({ card: card("adopted", "w1", "working", "adopted", 0), status: {}, warning: "Exit that conversation in its terminal first." })),
  workspace_import_planner_dir: vi.fn(async (a: { dir?: string; dry_run?: boolean } = {}): Promise<unknown> => ({
    dir: a.dir ?? "", dry_run: a.dry_run !== false, cards: [], created: 0, skipped: 0, messages: 0, questions: 0, gates: 0, unmatched_log_lines: 0, warnings: [],
  })),
  planner_adopt: vi.fn(async (): Promise<unknown> => ({ status: {}, warning: "Exit that conversation in its terminal first." })),
  logs_tail: vi.fn(async (): Promise<unknown[]> => []),
  diagnostics_get: vi.fn(async () => ({ version: state.fx.version, macos: "15.0", arch: "arm64", home: "/tmp", log_dir: "/tmp/hadron/logs", debug_logging: false, actions: 25, settings: {}, recent_log_lines: [] as string[] })),
  debug_logging_set: vi.fn(async (a: { enabled: boolean }) => ({ enabled: a.enabled, log_dir: "/tmp/hadron/logs" })),
  feedback_list: vi.fn(async (_a: { status?: string } = {}): Promise<Feedback[]> => []),
  feedback_create: vi.fn(async (a: { kind: string; text: string; area?: string; attach_diagnostics?: boolean }) => ({ id: "f1", created_at: ts, kind: a.kind, text: a.text, area: a.area ?? "", version: "v0.0.1-test", diagnostics: null, status: "new", note: "", github_url: "", issue_url: "" }) as Feedback),
  feedback_update: vi.fn(async (a: { id: string }) => ({ id: a.id }) as Feedback),
  client_log: vi.fn(async () => ({})),
  onChanged: vi.fn(() => () => {}),
  onEvent: vi.fn(() => () => {}),
  onNotify: vi.fn(() => () => {}),
  get_update_status: vi.fn(async () => clone(state.update)),
  check_for_update: vi.fn(async () => clone(state.update)),
  start_update: vi.fn(async () => ({})),
  set_update_token: vi.fn(async () => clone(state.update)),
};

export const apiMock = fns;
export const mocks = fns;
