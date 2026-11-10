import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import type { Routine } from "./api";
import { describeSchedule, draftOf, newRoutineDraft, scheduleOf } from "./components/RoutinesTab";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const routine = (over: Partial<Routine>): Routine => ({
  id: "rt1", created_at: "", updated_at: "", workspace_id: "w1", name: "Standup", enabled: true, cron: "0 9 * * 1-5", every_minutes: 0,
  target: "planner", card_id: "", prompt: "Post a standup.", catch_up: true, next_run_at: "2026-11-11T12:00:00Z", last_run_at: null, last_result: "", ...over,
});

async function openRoutines() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Routines" }));
  await screen.findByRole("button", { name: "Create routine" });
  return user;
}

describe("routine schedules", () => {
  it("turns the form's choices into cron or every_minutes, and refuses what the backend would", () => {
    const d = newRoutineDraft();
    expect(scheduleOf({ ...d, when: "weekdays", time: "09:30" })).toEqual({ cron: "30 9 * * 1-5" });
    expect(scheduleOf({ ...d, when: "daily", time: "18:05" })).toEqual({ cron: "5 18 * * *" });
    expect(scheduleOf({ ...d, when: "every", every: "2", unit: "hours" })).toEqual({ every_minutes: 120 });
    expect(scheduleOf({ ...d, when: "every", every: "3", unit: "minutes" })).toBe("A routine runs at most every 5 minutes.");
    expect(scheduleOf({ ...d, when: "cron", cron: " 0 8 * * 1 " })).toEqual({ cron: "0 8 * * 1" });
    expect(typeof scheduleOf({ ...d, when: "weekdays", time: "25:00" })).toBe("string");
  });

  it("describes a schedule in words and edits it back into the same form", () => {
    expect(describeSchedule({ cron: "0 9 * * 1-5", every_minutes: 0 })).toBe("Weekdays at 09:00");
    expect(describeSchedule({ cron: "5 18 * * *", every_minutes: 0 })).toBe("Every day at 18:05");
    expect(describeSchedule({ cron: "", every_minutes: 120 })).toBe("Every 2 hours");
    expect(describeSchedule({ cron: "", every_minutes: 45 })).toBe("Every 45 minutes");
    expect(describeSchedule({ cron: "0 8 * * 1", every_minutes: 0 })).toBe("cron 0 8 * * 1");
    expect(draftOf(routine({ cron: "5 18 * * *" }))).toMatchObject({ when: "daily", time: "18:05" });
    expect(draftOf(routine({ cron: "", every_minutes: 120 }))).toMatchObject({ when: "every", every: "2", unit: "hours" });
    expect(draftOf(routine({ target: "card", card_id: "c2" }))).toMatchObject({ target: "c2" });
  });
});

describe("Settings → Routines", () => {
  it("creates a weekday routine for a card with the right payload and lists it", async () => {
    const user = await openRoutines();
    await user.type(screen.getByRole("textbox", { name: "Routine name" }), "Morning check");
    await user.selectOptions(screen.getByRole("combobox", { name: "Send to" }), "c2");
    await user.type(screen.getByRole("textbox", { name: "Prompt" }), "Check the overnight errors.");
    await user.click(screen.getByRole("button", { name: "Create routine" }));
    await waitFor(() =>
      expect(mocks.routine_create).toHaveBeenCalledWith({ workspace_id: "w1", name: "Morning check", prompt: "Check the overnight errors.", catch_up: true, target: "card", card: "c2", cron: "0 9 * * 1-5" }),
    );
    const list = screen.getByLabelText("Routines");
    expect(await within(list).findByText("Morning check")).toBeInTheDocument();
    expect(within(list).getByText(/Weekdays at 09:00 · to api-timeouts/)).toBeInTheDocument();
  });

  it("keeps Create off until it has a name, a valid schedule and a prompt", async () => {
    const user = await openRoutines();
    const create = screen.getByRole("button", { name: "Create routine" });
    expect(create).toBeDisabled();
    await user.type(screen.getByRole("textbox", { name: "Routine name" }), "Ping");
    await user.selectOptions(screen.getByRole("combobox", { name: "When" }), "every");
    await user.clear(screen.getByRole("spinbutton", { name: "How often" }));
    await user.type(screen.getByRole("spinbutton", { name: "How often" }), "2");
    await user.type(screen.getByRole("textbox", { name: "Prompt" }), "Anything new?");
    expect(create).toBeDisabled();
    expect(create.title).toBe("A routine runs at most every 5 minutes.");
    await user.clear(screen.getByRole("spinbutton", { name: "How often" }));
    await user.type(screen.getByRole("spinbutton", { name: "How often" }), "15");
    expect(create).toBeEnabled();
  });

  it("pauses, runs now, edits and deletes a routine", async () => {
    state.routines = [routine({})];
    const user = await openRoutines();
    await user.click(await screen.findByRole("button", { name: "Pause Standup" }));
    await waitFor(() => expect(mocks.routine_update).toHaveBeenCalledWith({ routine_id: "rt1", enabled: false }));
    expect(await screen.findByTestId("routine-status-rt1")).toHaveTextContent("Paused");

    await user.click(screen.getByRole("button", { name: "Run Standup now" }));
    await waitFor(() => expect(mocks.routine_run).toHaveBeenCalledWith({ routine_id: "rt1" }));
    await waitFor(() => expect(screen.getByTestId("routine-status-rt1")).toHaveTextContent("sent the prompt to the running session"));

    await user.click(screen.getByRole("button", { name: "Edit Standup" }));
    expect(screen.getByRole("textbox", { name: "Routine name" })).toHaveValue("Standup");
    await user.selectOptions(screen.getByRole("combobox", { name: "When" }), "every");
    await user.clear(screen.getByRole("spinbutton", { name: "How often" }));
    await user.type(screen.getByRole("spinbutton", { name: "How often" }), "30");
    await user.click(screen.getByRole("button", { name: "Save routine" }));
    await waitFor(() =>
      expect(mocks.routine_update).toHaveBeenCalledWith({ routine_id: "rt1", name: "Standup", prompt: "Post a standup.", catch_up: true, target: "planner", cron: "", every_minutes: 30 }),
    );

    await user.click(screen.getByRole("button", { name: "Delete Standup" }));
    await waitFor(() => expect(mocks.routine_delete).toHaveBeenCalledWith({ routine_id: "rt1" }));
    expect(await screen.findByText(/No routines in Acme yet/)).toBeInTheDocument();
  });
});
