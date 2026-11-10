// Typed client for the action registry. Every UI mutation goes through here, and so through
// App.Invoke. The types are hand-written to match internal/domain and internal/actions; a Vitest
// test checks that every action in internal/actions/testdata/actions.json has a wrapper below.
import { Invoke } from "../wailsjs/go/app/App";
import { EventsOn } from "../wailsjs/runtime/runtime";

export type Role = "needs-you" | "active" | "blocked" | "done" | "backlog" | "";
export type Liveness = "working" | "idle" | "waiting" | "ended";

export interface Workspace {
  id: string;
  created_at: string;
  updated_at: string;
  name: string;
  color: string;
  root: string;
  position: number;
  archived: boolean;
  integrations: string[];
  filter_bar: string[];
  /** The model cards use when neither the card nor its launch profile names one; empty means claude's own default. */
  default_model: string;
  /** The planner's model; empty falls back to default_model. */
  planner_model: string;
  /** The folder the planner runs in; empty means root. */
  planner_cwd?: string;
  ui: {
    collapsed?: { planner?: boolean; card?: boolean };
    full?: "planner" | "card";
    main_view?: "board" | "dashboard";
    [k: string]: unknown;
  };
}

export interface Column {
  id: string;
  workspace_id: string;
  name: string;
  role: Role;
  position: number;
  wip: number;
  fold: boolean;
  color: string;
}

export interface Card {
  id: string;
  workspace_id: string;
  column_id: string;
  position: number;
  nick: string;
  key: string;
  title: string;
  assignee: string;
  stage: string;
  liveness: Liveness;
  return_column_id: string;
  launch_profile: string;
  fields: Record<string, unknown>;
  archived: boolean;
  /** The card layer of its session prompt. */
  prompt: string;
  /** The Claude model of this card's session; empty falls back to the launch profile's, then the workspace default. */
  model: string;
  /** The folder the card's session runs in; empty means the workspace root. */
  cwd?: string;
}

export interface AppStatus {
  version: string;
  home: string;
  actions: number;
  schema_version: number;
}

export interface ColumnDeleteOut {
  deleted: Column;
  moved_to: Column;
  card_count: number;
}

export interface ChangedEvent {
  workspace_id: string;
  entities: string[];
}

export async function call<T>(name: string, args: object = {}): Promise<T> {
  const out = await Invoke(name, JSON.stringify(args));
  return (out ? JSON.parse(out) : null) as T;
}

/** Subscribes to the backend's `changed` event. Returns the unsubscribe function. */
export function onChanged(cb: (e: ChangedEvent) => void): () => void {
  return EventsOn("changed", (e: ChangedEvent) => cb(e));
}

// ---- app and settings ----
export const app_status = () => call<AppStatus>("app_status");
export const settings_get = (a: { key?: string } = {}) => call<Record<string, unknown>>("settings_get", a);
export const settings_update = (a: { values: Record<string, unknown> }) => call<Record<string, unknown>>("settings_update", a);

// ---- workspaces ----
export const workspace_list = (a: { include_archived?: boolean } = {}) => call<Workspace[]>("workspace_list", a);
export const workspace_get = (a: { workspace_id: string }) => call<Workspace>("workspace_get", a);
export const workspace_create = (a: {
  name: string;
  root?: string;
  color?: string;
  template?: "planner" | "simple" | "copy";
  copy_from?: string;
  integrations?: string[];
}) => call<Workspace>("workspace_create", a);
export const workspace_update = (a: {
  workspace_id: string;
  name?: string;
  root?: string;
  color?: string;
  integrations?: string[];
  filter_bar?: string[];
  default_model?: string;
  planner_cwd?: string;
}) => call<Workspace>("workspace_update", a);
export const workspace_archive = (a: { workspace_id: string; archived?: boolean }) => call<Workspace>("workspace_archive", a);
export const workspace_delete = (a: { workspace_id: string }) => call<Workspace>("workspace_delete", a);
export const workspace_switch = (a: { workspace_id: string }) => call<Workspace>("workspace_switch", a);

// ---- columns ----
export const column_list = (a: { workspace_id?: string } = {}) => call<Column[]>("column_list", a);
export const column_create = (a: { workspace_id?: string; name: string; role?: Role; wip?: number; color?: string }) =>
  call<Column>("column_create", a);
export const column_update = (a: { column_id: string; name?: string; role?: Role; wip?: number; fold?: boolean; collapsed?: boolean; color?: string }) =>
  call<Column>("column_update", a);
export const column_reorder = (a: { workspace_id?: string; column_ids: string[] }) => call<Column[]>("column_reorder", a);
export const column_delete = (a: { column_id: string; move_to?: string }) => call<ColumnDeleteOut>("column_delete", a);

// ---- cards ----
export const card_list = (a: {
  workspace_id?: string;
  column_id?: string;
  role?: Role;
  liveness?: Liveness;
  expr?: string;
  include_archived?: boolean;
} = {}) => call<Card[]>("card_list", a);
export const card_get = (a: { card_id: string }) => call<Card>("card_get", a);
export const card_create = (a: {
  workspace_id?: string;
  nick: string;
  key?: string;
  title?: string;
  column_id?: string;
  assignee?: string;
  launch_profile?: string;
  start?: boolean;
  prompt?: string;
  model?: string;
  cwd?: string;
}) => call<Card>("card_create", a);
export const card_update = (a: {
  card_id: string;
  nick?: string;
  key?: string;
  title?: string;
  assignee?: string;
  stage?: string;
  launch_profile?: string;
  prompt?: string;
  model?: string;
  cwd?: string;
}) => call<Card & { note?: string }>("card_update", a);
export const card_move = (a: { card_id: string; column_id: string; position?: number }) => call<Card>("card_move", a);
export const card_archive = (a: { card_id: string; archived?: boolean }) => call<Card>("card_archive", a);
export const card_delete = (a: { card_id: string }) => call<Card>("card_delete", a);

// ---- sessions, planner and launch profiles ----
export interface Session {
  id: string;
  workspace_id: string;
  card_id: string;
  kind: "planner" | "card";
  command: string;
  cwd: string;
  pid: number;
  status: "running" | "exited";
  exit_code: number;
  claude_session_id: string;
  started_at: string;
  ended_at: string | null;
}
export interface SessionStatus {
  workspace_id: string;
  card_id: string;
  nick: string;
  running: boolean;
  /** "none" when the card has never had a session */
  liveness: Liveness | "none";
  session: Session | null;
  /** the planner's launch profile name (planner_get only); empty for none */
  launch_profile?: string;
  cols: number;
  rows: number;
  error: string;
}
export interface ScreenOut {
  running: boolean;
  liveness: Liveness;
  screen: string;
}
export interface LaunchProfile {
  id: string;
  workspace_id: string;
  name: string;
  command: string;
  args: string[];
  first_prompt: string;
  env: Record<string, string>;
  worktree: boolean;
  /** The launch profile layer of the session prompt. */
  prompt: string;
  /** The Claude model of sessions that use this profile, unless their card names one. */
  model: string;
}
/** A card is addressed by its id, nickname or key. */
export const session_start = (a: { card: string; launch_profile?: string }) => call<SessionStatus>("session_start", a);
export const session_restart = (a: { card: string; launch_profile?: string; fresh?: boolean }) => call<SessionStatus>("session_restart", a);
export const session_stop = (a: { card: string }) => call<SessionStatus>("session_stop", a);
export const session_send = (a: { card: string; text: string; enter?: boolean }) => call<SessionStatus>("session_send", a);
export const session_keys = (a: { card: string; keys: string[] }) => call<SessionStatus>("session_keys", a);
export const session_interrupt = (a: { card: string }) => call<SessionStatus>("session_interrupt", a);
export const session_read_screen = (a: { card: string; lines?: number; scrollback?: boolean }) => call<ScreenOut>("session_read_screen", a);
export const session_status = (a: { card: string }) => call<SessionStatus>("session_status", a);
export const planner_get = (a: { workspace_id?: string } = {}) => call<SessionStatus>("planner_get", a);
export const planner_set_profile = (a: { workspace_id?: string; launch_profile?: string }) => call<SessionStatus>("planner_set_profile", a);
export const planner_start = (a: { workspace_id?: string } = {}) => call<SessionStatus>("planner_start", a);
export interface RunningSessions {
  count: number;
  /** "planner" or a card nickname */
  sessions: string[];
  restore_on_launch: boolean;
}
export const sessions_running = () => call<RunningSessions>("sessions_running", {});
export const planner_stop = (a: { workspace_id?: string } = {}) => call<SessionStatus>("planner_stop", a);
export const planner_restart = (a: { workspace_id?: string; fresh?: boolean } = {}) => call<SessionStatus>("planner_restart", a);
export const planner_send = (a: { workspace_id?: string; text: string; enter?: boolean }) => call<SessionStatus>("planner_send", a);
export const planner_keys = (a: { workspace_id?: string; keys: string[] }) => call<SessionStatus>("planner_keys", a);
export const planner_read_screen = (a: { workspace_id?: string; lines?: number; scrollback?: boolean }) =>
  call<ScreenOut>("planner_read_screen", a);
export const launch_profile_list = (a: { workspace_id?: string } = {}) => call<LaunchProfile[]>("launch_profile_list", a);
export const launch_profile_create = (a: {
  workspace_id?: string;
  name: string;
  command?: string;
  args?: string[];
  first_prompt?: string;
  env?: Record<string, string>;
  worktree?: boolean;
  prompt?: string;
  model?: string;
}) => call<LaunchProfile>("launch_profile_create", a);
export const launch_profile_update = (a: {
  profile_id: string;
  name?: string;
  command?: string;
  args?: string[];
  first_prompt?: string;
  env?: Record<string, string>;
  worktree?: boolean;
  prompt?: string;
  model?: string;
}) => call<LaunchProfile>("launch_profile_update", a);
export const launch_profile_delete = (a: { profile_id: string }) => call<LaunchProfile>("launch_profile_delete", a);

// ---- changelog ----
export interface ChangelogSection {
  title: string;
  items: string[];
}
export interface ChangelogRelease {
  /** 0.2.0, or Unreleased */
  version: string;
  date: string;
  sections: ChangelogSection[];
  /** The section as markdown. */
  body: string;
}
export interface ChangelogInfo {
  running: string;
  found: boolean;
  release: ChangelogRelease;
  highlights: string[];
  releases: { version: string; date: string; summary: string }[];
  /** The version whose notes the user already saw. */
  seen: string;
  /** True when What's new should open now: a new release nobody has seen, never a dev build. */
  show: boolean;
}
export const changelog_get = (a: { version?: string } = {}) => call<ChangelogInfo>("changelog_get", a);

// ---- attachments ----
/** A file kept under ~/.hadron/attachments. url is set for an image: the window loads its thumbnail from it. */
export interface Attachment {
  path: string;
  url: string;
  mime: string;
  size: number;
  name: string;
}
/** data is the file's bytes in base64. Only the user attaches files. */
export const attachment_save = (a: { name?: string; data: string }) => call<Attachment>("attachment_save", a);

// ---- models ----
export interface ModelOption {
  id: string;
  label: string;
}
export interface ModelsInfo {
  models: ModelOption[];
  workspace_id: string;
  default_model: string;
  planner_model: string;
  order: string;
}
export interface ModelSetResult {
  card?: Card;
  model: string;
  source: string;
  typed: boolean;
  note: string;
}
export const models_list = (a: { workspace_id?: string } = {}) => call<ModelsInfo>("models_list", a);
export const models_set = (a: { models: ModelOption[] }) => call<ModelsInfo>("models_set", a);
export const card_set_model = (a: { card: string; model: string }) => call<ModelSetResult>("card_set_model", a);
export const planner_set_model = (a: { workspace_id?: string; model: string }) => call<ModelSetResult>("planner_set_model", a);

// ---- ui state ----
export const ui_panel_collapse = (a: { workspace_id?: string; panel: "planner" | "card"; collapsed: boolean }) =>
  call<Workspace>("ui_panel_collapse", a);
export const ui_fullscreen = (a: { workspace_id?: string; panel?: "planner" | "card" | "" }) => call<Workspace>("ui_fullscreen", a);
export const ui_main_view = (a: { workspace_id?: string; view: "board" | "dashboard" }) => call<Workspace>("ui_main_view", a);

// ---- chat ----
export type ReportKind = "progress" | "question" | "blocked" | "done";
/** command: a slash command you sent from the chat straight to the session's terminal (chat_command). */
export type MessageKind = "text" | "report" | "widget" | "event" | "answer" | "command";
export interface Message {
  id: string;
  created_at: string;
  updated_at: string;
  workspace_id: string;
  /** "planner" or "card:<card id>" */
  channel: string;
  author: "you" | "planner" | "session" | "rule" | "system";
  kind: MessageKind;
  report_kind: ReportKind | "";
  text: string;
  widget_title: string;
  widget_html: string;
  delivered: boolean;
  /** why the planner did not receive it, such as planner_not_running */
  reason: string;
  /** set on a question's report and on its answer */
  question_id: string;
}
export interface ChannelSummary {
  channel: string;
  count: number;
  last_at: string;
  last_author: string;
  last_kind: string;
  last_text: string;
}
export const chat_list = (a: { workspace_id?: string } = {}) => call<ChannelSummary[]>("chat_list", a);
export const chat_read = (a: { workspace_id?: string; channel: string; since?: string; limit?: number }) => call<Message[]>("chat_read", a);
export const chat_send = (a: { workspace_id?: string; channel: string; text: string }) => call<Message>("chat_send", a);
/** Types a /command into the channel's session terminal (a card's, or the planner's) and stores it; never relayed to the planner. */
export const chat_command = (a: { workspace_id?: string; channel: string; text: string }) => call<Message>("chat_command", a);
export const chat_post = (a: { workspace_id?: string; channel: string; text: string }) => call<Message>("chat_post", a);

// ---- the composer's / and @ menus ----
/** Whose folder and commands the composer completes against: a card, or the planner. */
export interface ComposerTarget {
  workspace_id?: string;
  card?: string;
  planner?: boolean;
}
export interface SlashCommand {
  /** without the slash; a command in a subfolder is "ns:cmd" */
  name: string;
  description: string;
  source: "built-in" | "project" | "user";
  kind: "command" | "skill";
}
export interface FileHit {
  /** relative to the session's folder */
  path: string;
  is_dir: boolean;
}
export const session_commands_list = (a: ComposerTarget) => call<SlashCommand[]>("session_commands_list", a);
export const files_search = (a: ComposerTarget & { q?: string; limit?: number }) => call<FileHit[]>("files_search", a);
export const widget_post = (a: { workspace_id?: string; channel: string; title: string; html: string }) => call<Message>("widget_post", a);
export const card_report = (a: { card?: string; kind: ReportKind; text: string }) => call<Message>("card_report", a);

// ---- questions, gates and the planner policy ----
export type QuestionKind = "decision" | "unblock" | "approval";
export interface QuestionOption {
  label: string;
  description: string;
}
/** Can an approved operation be undone? Older stored questions hold true or false. */
export type Reversible = "yes" | "no" | "partial";
export interface Approval {
  target: string;
  runs_as: string;
  reversible: Reversible | boolean;
  /** How it is undone, or what stays after an undo; set when reversible is partial. */
  reversible_note?: string;
  command: string;
  /** The command's language for highlighting, such as sql or bash. */
  command_lang?: string;
}
export interface Answer {
  selections: string[];
  notes: string;
  /** you, planner or external */
  via: string;
  /** planner-relay when the planner relayed the user's chat answer. */
  source_message_id?: string;
}
export interface Question {
  id: string;
  created_at: string;
  updated_at: string;
  workspace_id: string;
  card_id: string;
  /** 1 for Q-01 */
  seq: number;
  kind: QuestionKind;
  text: string;
  context: string;
  options: QuestionOption[];
  multi: boolean;
  approval: Approval | null;
  status: "open" | "answered" | "withdrawn";
  answer: Answer | null;
  answered_at: string | null;
  /** What the session does while the question waits. */
  meanwhile: string;
}
export type GateKind = "merge" | "promote" | "deploy-confirm" | "close";
export interface Gate {
  id: string;
  workspace_id: string;
  card_id: string;
  kind: GateKind;
  summary: string;
  since: string;
}
export type PlannerPolicy = "allow_all" | "decisions_only" | "ask_for_prd" | "user_only";
export const question_ask = (a: {
  card?: string;
  kind: QuestionKind;
  text: string;
  context?: string;
  options?: QuestionOption[];
  multi?: boolean;
  approval?: Approval;
  meanwhile?: string;
}) => call<Question>("question_ask", a);
export const question_list = (a: { workspace_id?: string; card?: string; status?: "open" | "answered" | "withdrawn" } = {}) =>
  call<Question[]>("question_list", a);
export const question_get = (a: { question_id: string }) => call<Question>("question_get", a);
export const question_answer = (a: {
  question_id: string;
  selections?: string[];
  notes?: string;
  /** Planner only: "user" relays an answer the user gave in chat, with source_message_id. */
  on_behalf_of?: "user";
  source_message_id?: string;
}) => call<Question>("question_answer", a);
export const question_withdraw = (a: { question_id: string; reason?: string }) => call<Question>("question_withdraw", a);
export const gate_set = (a: { card?: string; kind: GateKind; summary: string }) => call<Gate>("gate_set", a);
export const gate_list = (a: { workspace_id?: string; card?: string } = {}) => call<Gate[]>("gate_list", a);
export const gate_withdraw = (a: { gate_id?: string; card?: string; kind?: GateKind; reason?: string }) => call<Gate>("gate_withdraw", a);
export const gate_clear = (a: { gate_id?: string; card?: string; kind?: GateKind }) => call<Gate>("gate_clear", a);
export interface PlannerPolicyInfo {
  /** The policy in force. */
  policy: PlannerPolicy;
  /** workspace, global or default */
  source: "workspace" | "global" | "default";
  global: PlannerPolicy;
  workspace_id?: string;
  /** The workspace's own policy, when it has one. */
  override?: PlannerPolicy;
  options: PlannerPolicy[];
}
export const planner_policy_get = (a: { workspace_id?: string } = {}) => call<PlannerPolicyInfo>("planner_policy_get", a);
/** Without workspace_id it sets the global policy; with it, that workspace's own ("inherit" removes it). */
export const planner_policy_set = (a: { policy: PlannerPolicy | "inherit"; workspace_id?: string }) => call<PlannerPolicyInfo>("planner_policy_set", a);

// ---- MCP server ----
export interface McpInfo {
  running: boolean;
  url: string;
  port: number;
  token: string;
  tool_count: number;
  connect_command: string;
}
export interface McpCall {
  id: string;
  created_at: string;
  workspace_id: string;
  /** you, planner, session:<nick>, external or rule:<name> */
  actor: string;
  tool: string;
  args: unknown;
  ok: boolean;
  error: string;
  duration_ms: number;
}
export const mcp_info = () => call<McpInfo>("mcp_info", {});
export const mcp_calls = (a: { workspace_id?: string; limit?: number } = {}) => call<McpCall[]>("mcp_calls", a);

// ---- updates ----
export interface UpdateRelease {
  tag: string;
  name: string;
  notes: string;
  /** milliseconds since the epoch */
  date: number;
  url: string;
}
export interface UpdateStatus {
  version: string;
  repo: string;
  latest: UpdateRelease | null;
  upToDate: boolean;
  hasToken: boolean;
  state: "idle" | "checking" | "downloading" | "verifying" | "installing" | "failed" | string;
  message: string;
  lines: string[];
  seq: number;
  installedApp: string;
}
export const get_update_status = () => call<UpdateStatus>("get_update_status", {});
export const check_for_update = () => call<UpdateStatus>("check_for_update", {});
export const start_update = () => call<Record<string, never>>("start_update", {});
export const set_update_token = (a: { token?: string }) => call<UpdateStatus>("set_update_token", a);

/** Subscribe to any Wails event, such as update:output. Returns the unsubscribe function. */
export function onEvent(event: string, cb: (data: unknown) => void): () => void {
  return EventsOn(event, cb);
}

// ---- fields, filters ----
export type FieldType = "text" | "number" | "enum" | "user" | "date" | "bool";
export interface FieldEntry {
  path: string;
  label: string;
  type: FieldType;
  options?: string[];
  filterable: boolean;
  editable: boolean;
  card_chip: boolean;
  source: string;
}
export interface FieldSchema {
  fields: FieldEntry[];
  namespaces: string[];
}
export const field_schema_get = (a: { workspace_id?: string } = {}) => call<FieldSchema>("field_schema_get", a);
export const card_fields_get = (a: { card: string }) => call<{ card_id: string; fields: Record<string, unknown> }>("card_fields_get", a);
export const card_field_set = (a: { card: string; path: string; value: unknown }) =>
  call<{ card: Card; path: string; old: unknown; value: unknown }>("card_field_set", a);
export interface FilterSpec {
  vals?: Record<string, string>;
  q?: string;
  expr?: string;
}
export const filter_eval = (a: { workspace_id?: string; expr?: string; vals?: Record<string, string>; q?: string; include_archived?: boolean }) =>
  call<{ card_ids: string[] }>("filter_eval", a);
export const filter_bar_set = (a: { workspace_id?: string; fields: string[] }) => call<Workspace>("filter_bar_set", a);

// ---- views ----
export interface View {
  id: string;
  created_at: string;
  updated_at: string;
  workspace_id: string;
  name: string;
  filters: FilterSpec;
  position: number;
}
export const view_list = (a: { workspace_id?: string } = {}) => call<View[]>("view_list", a);
export const view_save = (a: { workspace_id?: string; view_id?: string; name: string; filters: FilterSpec }) => call<View>("view_save", a);
export const view_apply = (a: { view_id: string }) => call<{ view: View; card_ids: string[] }>("view_apply", a);
export const view_delete = (a: { view_id: string }) => call<View>("view_delete", a);

// ---- prompts ----
export type PromptKind = "planner" | "session" | "handoff" | "handoff_planner";
export interface PromptInfo {
  kind: PromptKind;
  template: string;
  default: string;
  /** The built-in harness text under this kind's workspace prompt; read-only, empty for handoffs. */
  core: string;
  core_version: number;
  custom: boolean;
  variables: string[];
  unknown: string[];
  warning?: string;
}
export const prompt_get = (a: { workspace_id?: string; kind: PromptKind }) => call<PromptInfo>("prompt_get", a);
export const prompt_set = (a: { workspace_id?: string; kind: PromptKind; template: string }) => call<PromptInfo>("prompt_set", a);
export const prompt_reset = (a: { workspace_id?: string; kind: PromptKind }) => call<PromptInfo>("prompt_reset", a);
export const prompt_render = (a: { workspace_id?: string; kind: PromptKind; card?: string; template?: string }) =>
  call<{ text: string; unknown: string[] }>("prompt_render", a);
export interface PromptLayer {
  name: "core" | "workspace" | "profile" | "card";
  title: string;
  editable: boolean;
  template: string;
  rendered?: string;
}
export const prompt_layers_get = (a: { workspace_id?: string; kind: PromptKind; card?: string }) =>
  call<{ kind: PromptKind; core_version: number; layers: PromptLayer[]; text: string }>("prompt_layers_get", a);
export const hadron_guide = (a: { topic?: string } = {}) =>
  call<{ topic: string; text: string; topics: string[]; core_version: number }>("hadron_guide", a);
export const custom_field_list = (a: { workspace_id?: string } = {}) => call<FieldEntry[]>("custom_field_list", a);
export const custom_field_create = (a: {
  workspace_id?: string;
  name: string;
  label?: string;
  type?: FieldType;
  options?: string[];
  card_chip?: boolean;
}) => call<FieldEntry>("custom_field_create", a);
export const custom_field_update = (a: { workspace_id?: string; name: string; label?: string; options?: string[]; card_chip?: boolean }) =>
  call<FieldEntry>("custom_field_update", a);
export const custom_field_delete = (a: { workspace_id?: string; name: string }) => call<FieldEntry>("custom_field_delete", a);

// ---- rules ----
export type TriggerOn =
  | "field_changed"
  | "column_entered"
  | "card_created"
  | "report"
  | "question_opened"
  | "question_answered"
  | "idle_for"
  | "no_report_for"
  | "cron";
export interface RuleTrigger {
  on: TriggerOn;
  field?: string;
  column?: string;
  kind?: string;
  minutes?: number;
  spec?: string;
}
export type RuleActionType = "move" | "set_field" | "notify" | "tell_planner" | "run_action";
export interface RuleAction {
  type: RuleActionType;
  column_id?: string;
  role?: Role;
  to?: string;
  remember_return?: boolean;
  path?: string;
  value?: unknown;
  title?: string;
  text?: string;
  script?: string;
  action?: string;
}
export interface Rule {
  id: string;
  created_at: string;
  updated_at: string;
  workspace_id: string;
  position: number;
  name: string;
  enabled: boolean;
  trigger: RuleTrigger;
  condition: string;
  actions: RuleAction[];
  builtin: boolean;
}
export interface DryRunItem {
  rule_id: string;
  rule: string;
  card_id: string;
  card: string;
  action: string;
  from: string;
  to: string;
  text: string;
  skipped: boolean;
  reason?: string;
}
export interface RuleLogEntry {
  id: string;
  at: string;
  rule: string;
  kind: "fire" | "skip";
  card_id: string;
  card: string;
  before: unknown;
  after: unknown;
  reason?: string;
}
export const rule_list = (a: { workspace_id?: string } = {}) => call<Rule[]>("rule_list", a);
export const rule_create = (a: {
  workspace_id?: string;
  name: string;
  trigger: RuleTrigger;
  condition?: string;
  actions: RuleAction[];
  enabled?: boolean;
}) => call<Rule>("rule_create", a);
export const rule_update = (a: {
  rule_id: string;
  name?: string;
  trigger?: RuleTrigger;
  condition?: string;
  actions?: RuleAction[];
  enabled?: boolean;
}) => call<Rule>("rule_update", a);
export const rule_reorder = (a: { workspace_id?: string; rule_ids: string[] }) => call<Rule[]>("rule_reorder", a);
export const rule_delete = (a: { rule_id: string }) => call<Rule>("rule_delete", a);
export const rule_dry_run = (a: { workspace_id?: string; rule_id?: string } = {}) => call<DryRunItem[]>("rule_dry_run", a);
export const rule_log = (a: { workspace_id?: string; limit?: number } = {}) => call<RuleLogEntry[]>("rule_log", a);
export const notify = (a: { title: string; body?: string }) => call<{ title: string; body: string }>("notify", a);

export interface NotifyEvent {
  workspace_id: string;
  card_id: string;
  title: string;
  body: string;
}
/** Subscribes to the backend's `notify` event, which a rule's notify action emits. */
export function onNotify(cb: (e: NotifyEvent) => void): () => void {
  return EventsOn("notify", (e: NotifyEvent) => cb(e));
}

// ---- integrations, scripts, links, remote sessions (WI-18) ----

export interface ScriptFieldDef {
  name: string;
  label?: string;
  type: string;
  options?: string[];
  filterable?: boolean;
  editable?: boolean;
  card_chip?: boolean;
}
export interface ScriptPermissions {
  network?: string[];
  secrets?: string[];
  api?: string[];
  exec?: string[];
}
export interface ScriptTestCase {
  name: string;
  op: string;
  ok: boolean;
  error?: string;
  output?: unknown;
}
export interface ScriptTestResult {
  ok: boolean;
  passed: number;
  failed: number;
  cases: ScriptTestCase[];
  ran_at: string;
}
export interface ScriptConfig {
  key: string;
  label: string;
  description: string;
  type: "string" | "number" | "bool";
  required: boolean;
  value: unknown;
}
export type ScriptStatus = "pending" | "enabled" | "disabled" | "rejected";
export interface ScriptView {
  id: string;
  name: string;
  kind: "integration" | "script";
  status: ScriptStatus;
  workspace_id: string;
  global: boolean;
  version: string;
  description: string;
  used_here: boolean;
  fields: ScriptFieldDef[];
  ops: string[];
  permissions: ScriptPermissions;
  new_domains: string[];
  config: ScriptConfig[];
  secrets: { name: string; set: boolean }[];
  tests: number;
  test_result: ScriptTestResult | null;
  poll_seconds: number;
  problems: string[];
  approved_by: string;
  approved_at: string | null;
  sessions: boolean;
  links: number;
}
export interface Link {
  id: string;
  workspace_id: string;
  card_id: string;
  integration: string;
  item_id: string;
  title: string;
  url: string;
  fields: Record<string, unknown>;
  synced_at: string | null;
}
export interface LinkItem {
  item_id: string;
  title: string;
  url: string;
  fields: Record<string, unknown>;
}
export interface RemoteSession {
  integration: string;
  id: string;
  title: string;
  status: string;
  url: string;
  item_id: string;
}
export interface ScriptLog {
  id: string;
  created_at: string;
  workspace_id: string;
  script_name: string;
  op: string;
  ok: boolean;
  duration_ms: number;
  message: string;
}
export interface RunOpResult {
  output: unknown;
  logs: string[];
  duration_ms: number;
}

type WS = { workspace_id?: string };
export const integration_list = (a: WS = {}) => call<ScriptView[]>("integration_list", a);
export const integration_templates = () => call<{ name: string; description: string }[]>("integration_templates", {});
export const integration_scaffold = (a: WS & { name: string; template?: string; global?: boolean }) => call<ScriptView>("integration_scaffold", a);
export const integration_write_file = (a: WS & { name: string; path: "manifest.json" | "index.js"; content: string }) => call<ScriptView>("integration_write_file", a);
export const integration_read_file = (a: WS & { name: string; path: "manifest.json" | "index.js" }) => call<{ path: string; content: string }>("integration_read_file", a);
export const integration_validate = (a: WS & { name: string }) => call<{ ok: boolean; problems: string[] }>("integration_validate", a);
export const integration_test = (a: WS & { name: string }) => call<ScriptTestResult>("integration_test", a);
export const integration_run_op = (a: WS & { name: string; op: string; input?: unknown }) => call<RunOpResult>("integration_run_op", a);
export const integration_approve = (a: WS & { name: string }) => call<ScriptView>("integration_approve", a);
export const integration_disable = (a: WS & { name: string }) => call<ScriptView>("integration_disable", a);
export const integration_remove = (a: WS & { name: string }) => call<{ removed: string; links_removed: number }>("integration_remove", a);
export const integration_logs = (a: { name?: string; limit?: number } = {}) => call<ScriptLog[]>("integration_logs", a);
export const integration_config_set = (a: { name: string; config: Record<string, unknown> }) => call<ScriptView>("integration_config_set", a);
export const integration_secret_set = (a: { name: string; secret: string; value: string }) => call<ScriptView>("integration_secret_set", a);
export const integration_secret_clear = (a: { name: string; secret: string }) => call<ScriptView>("integration_secret_clear", a);
export const integration_use = (a: WS & { name: string; enabled: boolean }) => call<ScriptView>("integration_use", a);

export const script_list = (a: WS = {}) => call<ScriptView[]>("script_list", a);
export const script_scaffold = (a: WS & { name: string; global?: boolean }) => call<ScriptView>("script_scaffold", a);
export const script_write_file = (a: WS & { name: string; path: "manifest.json" | "index.js"; content: string }) => call<ScriptView>("script_write_file", a);
export const script_read_file = (a: WS & { name: string; path: "manifest.json" | "index.js" }) => call<{ path: string; content: string }>("script_read_file", a);
export const script_validate = (a: WS & { name: string }) => call<{ ok: boolean; problems: string[] }>("script_validate", a);
export const script_test = (a: WS & { name: string }) => call<ScriptTestResult>("script_test", a);
export const script_run_op = (a: WS & { name: string; op: string; input?: unknown }) => call<RunOpResult>("script_run_op", a);
export const script_approve = (a: WS & { name: string }) => call<ScriptView>("script_approve", a);
export const script_disable = (a: WS & { name: string }) => call<ScriptView>("script_disable", a);
export const script_remove = (a: WS & { name: string }) => call<{ removed: string; links_removed: number }>("script_remove", a);
export const script_logs = (a: { name?: string; limit?: number } = {}) => call<ScriptLog[]>("script_logs", a);

export const link_list = (a: { card: string }) => call<Link[]>("link_list", a);
export const link_search = (a: { integration: string; q: string }) => call<{ items: LinkItem[] }>("link_search", a);
export const link_add = (a: { card: string; integration: string; item_id: string; title?: string; url?: string }) => call<Link>("link_add", a);
export const link_remove = (a: { link_id: string }) => call<Link>("link_remove", a);
export const link_refresh = (a: { link_id?: string; card?: string }) => call<{ links: Link[]; changed: string[] }>("link_refresh", a);
export const link_action_run = (a: { link_id: string; action: string; input?: unknown }) => call<RunOpResult>("link_action_run", a);

export const remote_session_list = (a: { integration?: string; card?: string } = {}) => call<RemoteSession[]>("remote_session_list", a);
export const remote_session_output = (a: { integration: string; id: string }) => call<{ text: string }>("remote_session_output", a);
export const remote_session_send = (a: { integration: string; id: string; text: string }) => call<unknown>("remote_session_send", a);
export const remote_session_start = (a: { integration: string; card?: string; item_id?: string; prompt?: string }) => call<RemoteSession>("remote_session_start", a);
export const remote_session_stop = (a: { integration: string; id: string }) => call<unknown>("remote_session_stop", a);
export const card_chips = (a: WS = {}) => call<Record<string, { path: string; label: string; value: unknown }[]>>("card_chips", a);

// ---- dashboard ----
export type BlockType = "stats" | "bars" | "table" | "heatmap" | "prose" | "planner" | "activity" | "custom";
export interface BlockSource {
  query?: string;
  args?: Record<string, unknown>;
  height?: number;
  columns?: { key: string; label: string }[];
  /** the chat message a pinned widget came from */
  from?: string;
  /** heatmap: "strip" (default for non-date keys) or "calendar" (chosen automatically when every key is YYYY-MM-DD) */
  layout?: "strip" | "calendar";
  /** heatmap calendar: weeks shown, 1 to 53 (default 26) */
  weeks?: number;
  /** heatmap: what is counted ("deploys"), for tooltips and the total line */
  unit?: string;
  [k: string]: unknown;
}
export interface Block {
  id: string;
  created_at: string;
  updated_at: string;
  workspace_id: string;
  position: number;
  type: BlockType;
  title: string;
  span: 1 | 2;
  source: BlockSource;
  html: string;
  schedule: string;
  text: string;
  text_at: string | null;
}
export interface DashResult {
  query: string;
  shape: "groups" | "rows";
  groups: { key: string; label: string; count: number }[];
  rows: Record<string, unknown>[];
  /** Queries an integration script defines are served from a cache: how old the data is, in seconds. */
  age_s?: number;
  /** nothing has been fetched yet; a fetch is under way */
  pending?: boolean;
  /** older than its time to live, being refreshed */
  stale?: boolean;
  /** why the last refresh failed; the data shown is the last good one */
  error?: string;
}
export interface BlockView extends Block {
  data: DashResult | null;
  error?: string;
}
export interface DashQueryInfo {
  name: string;
  shape: string;
  doc: string;
  args: unknown;
  any_args?: boolean;
}
export interface BlockSpec {
  type: BlockType;
  title: string;
  span?: 1 | 2;
  source?: BlockSource;
  html?: string;
  text?: string;
  schedule?: string;
}
export const dash_get = (a: { workspace_id?: string } = {}) => call<{ blocks: BlockView[]; queries: DashQueryInfo[] }>("dash_get", a);
export const dash_set = (a: { workspace_id?: string; blocks: BlockSpec[] }) => call<Block[]>("dash_set", a);
export const dash_block_add = (a: BlockSpec & { workspace_id?: string; position?: number }) => call<Block>("dash_block_add", a);
export const dash_block_update = (a: { block_id: string; title?: string; span?: 1 | 2; source?: BlockSource; html?: string; text?: string; schedule?: string }) =>
  call<Block>("dash_block_update", a);
export const dash_block_move = (a: { block_id: string; position: number }) => call<Block[]>("dash_block_move", a);
export const dash_block_remove = (a: { block_id: string }) => call<Block>("dash_block_remove", a);
export const dash_block_refresh = (a: { block_id: string }) => call<BlockView>("dash_block_refresh", a);
export const dash_query_run = (a: { workspace_id?: string; query: string; args?: Record<string, unknown> }) => call<DashResult>("dash_query_run", a);
export const widget_pin = (a: { message_id: string; title?: string }) => call<Block>("widget_pin", a);

// ---- config history (revert) ----

export interface HistoryEntry {
  id: string;
  at: string;
  actor: string;
  actor_kind: string;
  action: string;
  kind: string;
  summary: string;
  reverted: boolean;
  revertible: boolean;
  reason?: string;
}
export interface DiffEntry {
  field: string;
  before: unknown;
  after: unknown;
}
export const config_history = (a: { workspace_id?: string; limit?: number; actor?: string } = {}) => call<HistoryEntry[]>("config_history", a);
export const config_diff = (a: { change_id: string }) => call<{ change: HistoryEntry; diff: DiffEntry[] }>("config_diff", a);
export const config_revert = (a: { change_id: string }) => call<{ reverted: HistoryEntry; summary: string }>("config_revert", a);

// ---- card worktrees ----

export interface Worktree {
  nick: string;
  path: string;
  branch: string;
  dirty: boolean;
  card_id: string;
}
export const worktree_list = (a: { workspace_id?: string } = {}) => call<Worktree[]>("worktree_list", a);
export const worktree_remove = (a: { card: string; force?: boolean; workspace_id?: string }) =>
  call<{ removed: string; branch: string; note: string }>("worktree_remove", a);

// ---- adopting existing Claude Code conversations ----

export interface Adoptable {
  claude_session_id: string;
  first_user_message: string;
  last_activity: string;
  size: number;
  /** The folder the conversation was run in, read from its transcript. */
  cwd: string;
  already_in_hadron: boolean;
  held_by?: string;
}
export interface AdoptResult {
  card?: Card;
  status: Partial<SessionStatus>;
  warning: string;
}
export const session_adoptable_list = (a: { workspace_id?: string; cwd?: string; all?: boolean } = {}) => call<Adoptable[]>("session_adoptable_list", a);
export const session_adopt = (a: { workspace_id?: string; claude_session_id: string; nick: string; key?: string; column_id?: string; start?: boolean; handoff?: boolean; launch_profile?: string; cwd?: string }) =>
  call<AdoptResult>("session_adopt", a);
export const planner_adopt = (a: { workspace_id?: string; claude_session_id: string; handoff?: boolean; launch_profile?: string; cwd?: string }) =>
  call<AdoptResult>("planner_adopt", a);

// ---- importing a planner folder ----

export interface ImportCard {
  nick: string;
  key: string;
  action: "create" | "skip";
  reason?: string;
  column: string;
  stage: string;
  liveness: string;
  claude_session_id: string;
  messages: number;
  questions: number;
  open_questions: number;
  gates: number;
}
export interface ImportPlan {
  dir: string;
  dry_run: boolean;
  cards: ImportCard[];
  created: number;
  skipped: number;
  messages: number;
  questions: number;
  gates: number;
  unmatched_log_lines: number;
  warnings: string[];
}
export const workspace_import_planner_dir = (a: { workspace_id?: string; dir?: string; dry_run?: boolean; timeout_seconds?: number } = {}) =>
  call<ImportPlan>("workspace_import_planner_dir", a);

// ---- diagnostics ----

export interface LogLine {
  time: string;
  level: "debug" | "info" | "warn" | "error";
  msg: string;
  fields?: Record<string, unknown>;
  raw: string;
}
export interface Diagnostics {
  version: string;
  macos: string;
  arch: string;
  home: string;
  log_dir: string;
  debug_logging: boolean;
  actions: number;
  settings: Record<string, unknown>;
  recent_log_lines: string[];
}
export const client_log = (a: { level: "error" | "warn" | "info" | "debug"; message: string; stack?: string }) => call<unknown>("client_log", a);
export const logs_tail = (a: { lines?: number; level?: string; contains?: string } = {}) => call<LogLine[]>("logs_tail", a);
export const diagnostics_get = () => call<Diagnostics>("diagnostics_get", {});
export const debug_logging_set = (a: { enabled: boolean }) => call<{ enabled: boolean; log_dir: string }>("debug_logging_set", a);

// ---- feedback inbox ----

export type FeedbackKind = "bug" | "idea" | "annoyance" | "praise";
export type FeedbackStatus = "new" | "planned" | "in_progress" | "done" | "wontfix";
export interface Feedback {
  id: string;
  created_at: string;
  kind: FeedbackKind;
  text: string;
  area: string;
  version: string;
  diagnostics: unknown;
  status: FeedbackStatus;
  note: string;
  github_url: string;
  issue_url: string;
}
export const feedback_create = (a: { kind: FeedbackKind; text: string; area?: string; attach_diagnostics?: boolean }) => call<Feedback>("feedback_create", a);
export const feedback_list = (a: { status?: FeedbackStatus } = {}) => call<Feedback[]>("feedback_list", a);
export const feedback_update = (a: { id: string; status?: FeedbackStatus; note?: string; github_url?: string }) => call<Feedback>("feedback_update", a);

// ---- routines ----

/** A prompt sent to the planner or a card's session on a schedule. */
export interface Routine {
  id: string;
  created_at: string;
  updated_at: string;
  workspace_id: string;
  name: string;
  enabled: boolean;
  /** a five-field cron spec in local time, or empty when every_minutes is set */
  cron: string;
  every_minutes: number;
  target: "planner" | "card";
  card_id: string;
  prompt: string;
  catch_up: boolean;
  next_run_at: string | null;
  last_run_at: string | null;
  last_result: string;
}
export const routine_list = (a: { workspace_id?: string } = {}) => call<Routine[]>("routine_list", a);
export const routine_create = (a: {
  workspace_id?: string;
  name: string;
  cron?: string;
  every_minutes?: number;
  target?: "planner" | "card";
  card?: string;
  prompt: string;
  enabled?: boolean;
  catch_up?: boolean;
}) => call<Routine>("routine_create", a);
export const routine_update = (a: {
  routine_id: string;
  name?: string;
  cron?: string;
  every_minutes?: number;
  target?: "planner" | "card";
  card?: string;
  prompt?: string;
  enabled?: boolean;
  catch_up?: boolean;
}) => call<Routine>("routine_update", a);
export const routine_delete = (a: { routine_id: string }) => call<{ deleted: string }>("routine_delete", a);
export const routine_run = (a: { routine_id: string }) => call<Routine>("routine_run", a);
