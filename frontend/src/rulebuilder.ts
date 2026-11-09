import type { Column, FieldEntry, Rule, RuleAction, RuleTrigger, TriggerOn } from "./api";

export const TRIGGERS: { on: TriggerOn; label: string }[] = [
  { on: "field_changed", label: "a field changes" },
  { on: "column_entered", label: "a card enters a column" },
  { on: "card_created", label: "a card is created" },
  { on: "report", label: "a session reports" },
  { on: "question_opened", label: "a question opens" },
  { on: "question_answered", label: "the last question is answered" },
  { on: "idle_for", label: "a session stays idle" },
  { on: "no_report_for", label: "a session stops reporting" },
  { on: "cron", label: "the clock says" },
];

export const ACTIONS: { type: RuleAction["type"]; label: string }[] = [
  { type: "move", label: "move the card to" },
  { type: "set_field", label: "set a field" },
  { type: "notify", label: "notify me" },
  { type: "tell_planner", label: "tell the planner" },
];

export const REPORT_KINDS = ["progress", "question", "blocked", "done"];

/** What the rule builder holds while the user fills it in. */
export interface RuleDraft {
  name: string;
  on: TriggerOn;
  field: string; // field_changed
  column: string; // column_entered: a column id
  kind: string; // report
  minutes: string; // idle_for, no_report_for
  spec: string; // cron
  ifField: string; // "" means no condition
  ifOp: "is" | "not";
  ifValue: string;
  action: RuleAction["type"];
  toColumn: string; // move
  setPath: string; // set_field
  setValue: string;
  text: string; // notify, tell_planner
}

export function newDraft(columns: Column[], schema: FieldEntry[]): RuleDraft {
  const first = schema.find((f) => f.options?.length) ?? schema[0];
  return {
    name: "",
    on: "field_changed",
    field: first?.path ?? "",
    column: columns[0]?.id ?? "",
    kind: "",
    minutes: "30",
    spec: "0 9 * * 1-5",
    ifField: first?.path ?? "",
    ifOp: "is",
    ifValue: first?.options?.[0] ?? "",
    action: "move",
    toColumn: columns[0]?.id ?? "",
    setPath: schema.find((f) => f.editable)?.path ?? "",
    setValue: "",
    text: "",
  };
}

export interface RulePayload {
  name: string;
  trigger: RuleTrigger;
  condition?: string;
  actions: RuleAction[];
}

const q = (s: string) => JSON.stringify(s);

/** The CEL condition of the "If" row, or "" when there is none. */
export function conditionOf(d: RuleDraft): string {
  if (!d.ifField) return "";
  return `${d.ifField} ${d.ifOp === "is" ? "==" : "!="} ${q(d.ifValue)}`;
}

/** The payload of rule_create for a draft. Checked by the backend again; this only shapes it. */
export function buildRule(d: RuleDraft, columns: Column[]): RulePayload {
  const trigger: RuleTrigger = { on: d.on };
  if (d.on === "field_changed") trigger.field = d.field;
  if (d.on === "column_entered") trigger.column = d.column;
  if (d.on === "report" && d.kind) trigger.kind = d.kind;
  if (d.on === "idle_for" || d.on === "no_report_for") trigger.minutes = Math.max(1, parseInt(d.minutes || "0", 10) || 1);
  if (d.on === "cron") trigger.spec = d.spec.trim();

  let action: RuleAction;
  switch (d.action) {
    case "move":
      action = { type: "move", column_id: d.toColumn };
      break;
    case "set_field":
      action = { type: "set_field", path: d.setPath, value: d.setValue };
      break;
    default:
      action = { type: d.action, text: d.text.trim() };
  }
  const condition = conditionOf(d);
  const out: RulePayload = { name: d.name.trim() || autoName(d, columns), trigger, actions: [action] };
  if (condition) out.condition = condition;
  return out;
}

const colName = (columns: Column[], id: string) => columns.find((c) => c.id === id)?.name ?? id;

/** A name for a rule the user did not name: "status Finished goes to Done". */
export function autoName(d: RuleDraft, columns: Column[]): string {
  const subject = d.ifField ? `${d.ifField.split(".").slice(1).join(".")} ${d.ifOp === "is" ? "" : "not "}${d.ifValue}`.trim() : TRIGGERS.find((t) => t.on === d.on)?.label ?? d.on;
  switch (d.action) {
    case "move":
      return `${subject} goes to ${colName(columns, d.toColumn)}`;
    case "set_field":
      return `${subject} sets ${d.setPath}`;
    case "notify":
      return `${subject} notifies me`;
    default:
      return `${subject} tells the planner`;
  }
}

function triggerCel(t: RuleTrigger, columns: Column[]): string {
  switch (t.on) {
    case "field_changed":
      return `field_changed(${t.field || "any"})`;
    case "column_entered":
      return `column_entered(${t.column ? q(colName(columns, t.column)) : "any"})`;
    case "report":
      return `report(${t.kind || "any"})`;
    case "idle_for":
      return `idle_for(${t.minutes} min)`;
    case "no_report_for":
      return `no_report_for(${t.minutes} min)`;
    case "cron":
      return `cron(${q(t.spec ?? "")})`;
  }
  return t.on;
}

function actionCel(a: RuleAction, columns: Column[]): string {
  switch (a.type) {
    case "move":
      if (a.to === "return_column") return "card.move(return column)";
      if (a.role) return `card.move(role ${a.role})`;
      return `card.move(${q(colName(columns, a.column_id ?? ""))})`;
    case "set_field":
      return `card.set(${a.path}, ${JSON.stringify(a.value)})`;
    case "run_action":
      return `run_action(${a.script}.${a.action})`;
    default:
      return `${a.type}(${q(a.text ?? "")})`;
  }
}

/** A rule as one line of CEL-ish text: the trigger, the condition and the actions. */
export function celOfRule(r: Pick<Rule, "trigger" | "condition" | "actions">, columns: Column[]): string {
  const parts = [`on ${triggerCel(r.trigger, columns)}`];
  if (r.condition) parts.push(`if ${r.condition}`);
  return `${parts.join(" ")} → ${r.actions.map((a) => actionCel(a, columns)).join("; ")}`;
}

/** Whether the draft can be created: every part it uses is filled in. */
export function draftProblem(d: RuleDraft): string {
  if (d.on === "field_changed" && !d.field) return "Pick the field to watch.";
  if (d.on === "column_entered" && !d.column) return "Pick the column.";
  if (d.on === "cron" && !d.spec.trim()) return "Write a cron spec, such as 0 9 * * 1-5.";
  if (d.action === "move" && !d.toColumn) return "Pick the column to move to.";
  if (d.action === "set_field" && !d.setPath) return "Pick the field to set.";
  if ((d.action === "notify" || d.action === "tell_planner") && !d.text.trim()) return "Write the message.";
  return "";
}
