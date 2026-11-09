import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import type { Rule } from "./api";
import { autoName, buildRule, celOfRule, conditionOf, draftProblem, newDraft } from "./rulebuilder";
import type { RuleDraft } from "./rulebuilder";
import { col, defaultSchema, mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const rule = (over: Partial<Rule>): Rule => ({
  id: "r1", created_at: "", updated_at: "", workspace_id: "w1", position: 0, name: "Rule", enabled: true, builtin: false,
  trigger: { on: "card_created" }, condition: "", actions: [{ type: "notify", text: "hi" }], ...over,
});

async function openRules() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Rules" }));
  await screen.findByRole("button", { name: "Create rule" });
  await waitFor(() => expect(within(screen.getByRole("combobox", { name: "Field to watch" })).getAllByRole("option").length).toBeGreaterThan(1));
  return user;
}

describe("Settings → Rules builder", () => {
  it("creates 'jira.status is Finished moves the card to Done' with the right payload", async () => {
    const user = await openRules();
    await user.selectOptions(screen.getByRole("combobox", { name: "Field to watch" }), "jira.status");
    await user.selectOptions(screen.getByRole("combobox", { name: "Condition value" }), "Finished");
    await user.selectOptions(screen.getByRole("combobox", { name: "Move to column" }), "Done");
    expect(screen.getByLabelText("Rule as CEL").textContent).toBe('on field_changed(jira.status) if jira.status == "Finished" → card.move("Done")');

    await user.click(screen.getByRole("button", { name: "Create rule" }));
    await waitFor(() =>
      expect(mocks.rule_create).toHaveBeenCalledWith({
        workspace_id: "w1",
        name: "status Finished goes to Done",
        trigger: { on: "field_changed", field: "jira.status" },
        condition: 'jira.status == "Finished"',
        actions: [{ type: "move", column_id: "done" }],
      }),
    );
    expect(await screen.findByLabelText("Turn off status Finished goes to Done")).toBeInTheDocument();
  });

  it("uses the name the user typed, and builds a notify rule for a report", async () => {
    const user = await openRules();
    await user.type(screen.getByRole("textbox", { name: "Rule name" }), "Tell me about done");
    await user.selectOptions(screen.getByRole("combobox", { name: "When" }), "report");
    await user.selectOptions(screen.getByRole("combobox", { name: "Report kind" }), "done");
    await user.selectOptions(screen.getByRole("combobox", { name: "Condition field" }), "");
    await user.selectOptions(screen.getByRole("combobox", { name: "Then" }), "notify");
    expect(screen.getByRole("button", { name: "Create rule" })).toBeDisabled(); // no message yet
    await user.type(screen.getByRole("textbox", { name: "Message" }), "{{{{card.nick}} is done");
    await user.click(screen.getByRole("button", { name: "Create rule" }));
    await waitFor(() =>
      expect(mocks.rule_create).toHaveBeenCalledWith({
        workspace_id: "w1",
        name: "Tell me about done",
        trigger: { on: "report", kind: "done" },
        actions: [{ type: "notify", text: "{{card.nick}} is done" }],
      }),
    );
  });

  it("builds a no_report_for rule with minutes and shows the variables hint only for it", async () => {
    const user = await openRules();
    expect(screen.queryByLabelText("Variables")).toBeNull();
    await user.selectOptions(screen.getByRole("combobox", { name: "When" }), "no_report_for");
    const mins = screen.getByRole("spinbutton", { name: "Quiet minutes" });
    await user.clear(mins);
    await user.type(mins, "20");
    await user.selectOptions(screen.getByRole("combobox", { name: "Condition field" }), "");
    await user.selectOptions(screen.getByRole("combobox", { name: "Then" }), "tell_planner");
    await user.click(screen.getByRole("textbox", { name: "Message" }));
    await user.paste("{{card.nick}} is quiet");
    expect(screen.getByLabelText("Variables")).toHaveTextContent("{{quiet.minutes}}, {{report.last}}, {{report.last_kind}}, {{report.last_ago}}");
    expect(screen.getByLabelText("Rule as CEL").textContent).toContain("no_report_for(20 min)");
    await user.click(screen.getByRole("button", { name: "Create rule" }));
    await waitFor(() =>
      expect(mocks.rule_create).toHaveBeenCalledWith(
        expect.objectContaining({ trigger: { on: "no_report_for", minutes: 20 }, actions: [{ type: "tell_planner", text: "{{card.nick}} is quiet" }] }),
      ),
    );
  });

  it("shows the backend's reason when it refuses a rule", async () => {
    const user = await openRules();
    mocks.rule_create.mockRejectedValueOnce(new Error("condition: ERROR: <input>:1:1: undeclared reference to 'x'"));
    await user.click(screen.getByRole("button", { name: "Create rule" }));
    expect((await screen.findByRole("alert")).textContent).toContain("undeclared reference to 'x'");
  });

  it("lists rules with switches, built-ins cannot be deleted, and the order can change", async () => {
    state.rules = [
      rule({ id: "b1", name: "Open question moves the card", builtin: true, trigger: { on: "question_opened" }, actions: [{ type: "move", role: "needs-you", remember_return: true }] }),
      rule({ id: "r2", position: 1, name: "Finished goes to Done", trigger: { on: "field_changed", field: "jira.status" }, condition: 'jira.status == "Finished"', actions: [{ type: "move", column_id: "done" }] }),
    ];
    const user = await openRules();
    expect(screen.getByText("built-in")).toBeInTheDocument();
    expect(screen.getByText('on field_changed(jira.status) if jira.status == "Finished" → card.move("Done")')).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(1); // not on the built-in

    await user.click(screen.getByRole("button", { name: "Turn off Finished goes to Done" }));
    await waitFor(() => expect(mocks.rule_update).toHaveBeenCalledWith({ rule_id: "r2", enabled: false }));
    expect(await screen.findByRole("button", { name: "Turn on Finished goes to Done" })).toHaveAttribute("aria-pressed", "false");

    await user.click(screen.getByRole("button", { name: "Move Finished goes to Done up" }));
    await waitFor(() => expect(mocks.rule_reorder).toHaveBeenCalledWith({ workspace_id: "w1", rule_ids: ["r2", "b1"] }));

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mocks.rule_delete).toHaveBeenCalledWith({ rule_id: "r2" }));
    await waitFor(() => expect(screen.queryByText("Finished goes to Done")).not.toBeInTheDocument());
  });

  it("dry-runs the rules and shows what would change without applying", async () => {
    state.dryRun = [
      { rule_id: "r2", rule: "Finished goes to Done", card_id: "c2", card: "api-timeouts", action: "move", from: "Working", to: "Done", text: "", skipped: false },
      { rule_id: "r3", rule: "Ping", card_id: "c3", card: "dedupe-records", action: "move", from: "", to: "", text: "", skipped: true, reason: "loop guard: 10 rule actions" },
    ];
    const user = await openRules();
    await user.click(screen.getByRole("button", { name: "Dry run on the board" }));
    const box = await screen.findByLabelText("Dry run result");
    expect(within(box).getByText("api-timeouts")).toBeInTheDocument();
    expect(box.textContent).toContain("Finished goes to Done: move Working → Done");
    expect(box.textContent).toContain("skipped by Ping: loop guard");
    expect(mocks.rule_dry_run).toHaveBeenCalledWith({ workspace_id: "w1" });
    expect(mocks.card_move).not.toHaveBeenCalled();
    expect(await screen.findByRole("status")).toHaveTextContent("1 change(s) would happen");
  });
});

describe("rule builder payload", () => {
  const cols = [col("work", "w1", "Working", "active", 0), col("done", "w1", "Done", "done", 1)];
  const draft = (over: Partial<RuleDraft> = {}): RuleDraft => ({ ...newDraft(cols, defaultSchema()), ...over });

  it("builds a trigger for each kind", () => {
    expect(buildRule(draft({ on: "column_entered", column: "done", ifField: "" }), cols).trigger).toEqual({ on: "column_entered", column: "done" });
    expect(buildRule(draft({ on: "card_created", ifField: "" }), cols).trigger).toEqual({ on: "card_created" });
    expect(buildRule(draft({ on: "report", kind: "", ifField: "" }), cols).trigger).toEqual({ on: "report" });
    expect(buildRule(draft({ on: "idle_for", minutes: "45", ifField: "" }), cols).trigger).toEqual({ on: "idle_for", minutes: 45 });
    expect(buildRule(draft({ on: "cron", spec: " 0 9 * * 1-5 ", ifField: "" }), cols).trigger).toEqual({ on: "cron", spec: "0 9 * * 1-5" });
    expect(buildRule(draft({ on: "question_opened", ifField: "" }), cols).trigger).toEqual({ on: "question_opened" });
  });

  it("builds an action for each kind and leaves the condition out when there is none", () => {
    const d = draft({ ifField: "", action: "set_field", setPath: "card.stage", setValue: "review" });
    expect(buildRule(d, cols)).toMatchObject({ actions: [{ type: "set_field", path: "card.stage", value: "review" }] });
    expect("condition" in buildRule(d, cols)).toBe(false);
    expect(buildRule(draft({ action: "tell_planner", text: " look at {{card.nick}} " }), cols).actions).toEqual([{ type: "tell_planner", text: "look at {{card.nick}}" }]);
    expect(buildRule(draft({ ifField: "card.stage", ifOp: "not", ifValue: "x" }), cols).condition).toBe('card.stage != "x"');
    expect(conditionOf(draft({ ifField: "" }))).toBe("");
  });

  it("names a rule the user did not name, and says what is missing", () => {
    const d = draft({ ifField: "jira.status", ifOp: "is", ifValue: "Finished", toColumn: "done" });
    expect(autoName(d, cols)).toBe("status Finished goes to Done");
    expect(draftProblem(draft({ on: "cron", spec: " " }))).toMatch(/cron/);
    expect(draftProblem(draft({ action: "notify", text: "" }))).toMatch(/message/);
    expect(draftProblem(draft())).toBe("");
  });

  it("writes a rule as one line", () => {
    const r = { trigger: { on: "idle_for" as const, minutes: 30 }, condition: "", actions: [{ type: "tell_planner" as const, text: "stuck?" }, { type: "move" as const, to: "return_column" }] };
    expect(celOfRule(r, cols)).toBe('on idle_for(30 min) → tell_planner("stuck?"); card.move(return column)');
  });
});

describe("rule notifications", () => {
  it("shows a rule's notify event as a toast", async () => {
    let fire: (e: { workspace_id: string; card_id: string; title: string; body: string }) => void = () => {};
    mocks.onNotify.mockImplementation(((cb: typeof fire) => {
      fire = cb;
      return () => {};
    }) as never);
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    act(() => fire({ workspace_id: "w1", card_id: "c1", title: "Finished", body: "search-console reported done" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Finished: search-console reported done");
  });
});
