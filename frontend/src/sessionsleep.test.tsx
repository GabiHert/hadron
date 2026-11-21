import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { GeneralTab, SETTING_MEMORY_GUARD, SETTING_SLEEP_AFTER } from "./components/GeneralTab";
import css from "./styles/app.css?raw";
import { card, mocks, reset, running, state } from "./test/fakeApi";
import { resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

beforeEach(() => {
  reset();
  resetTerm();
});

/** dedupe-records' session sleeps. */
function asleep() {
  state.fx.cards.w1 = state.fx.cards.w1.map((c) => (c.id === "c3" ? card("c3", "w1", "working", "dedupe-records", 1, { liveness: "asleep" }) : c));
  state.sessions["c3"] = running("sess-c3", { card_id: "c3", nick: "dedupe-records", running: false, liveness: "asleep", asleep: true });
  state.sessions["c3"].session!.status = "exited";
  state.sessions["c3"].session!.asleep = true;
}

describe("a sleeping card", () => {
  it("says asleep on the board, quietly: a hollow dot and a dashed pill, not an error", async () => {
    asleep();
    render(<App />);
    const tile = await screen.findByRole("button", { name: "Open dedupe-records" });
    expect(within(tile).getByText("asleep")).toHaveClass("pill", "asleep");
    expect(within(tile).getByTitle("asleep")).toHaveClass("dot", "asleep");
    expect(within(tile).queryByText(/ended|error/i)).toBeNull();
    expect(css).toContain(".dot.asleep{background:transparent;box-shadow:inset 0 0 0 1.5px var(--fg3)}");
    expect(css).toMatch(/\.pill\.asleep\{color:var\(--fg3\);border-style:dashed\}/);
  });

  it("its terminal says it sleeps and how it wakes, keeps the terminal to type into, and Wake now wakes it", async () => {
    const user = userEvent.setup();
    asleep();
    render(<App />);
    await user.click(await screen.findByRole("button", { name: "Open dedupe-records" }));
    const panel = screen.getByRole("complementary", { name: "Card dedupe-records" });
    expect(within(panel).getByText("asleep")).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByText(/Asleep: Hadron stopped this session to save memory\. Type here or send it a message and it wakes/)).toBeInTheDocument();
    expect(within(panel).queryByText(/The session has ended/)).toBeNull();
    expect(within(panel).getByRole("group", { name: "dedupe-records terminal" })).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Wake now" }));
    expect(mocks.session_wake).toHaveBeenCalledWith({ card: "c3" });
  });
});

describe("Settings → General: memory", () => {
  const sleepSelect = () => screen.findByRole("combobox", { name: "Put idle card sessions to sleep" });
  const guardBox = () => screen.findByRole("checkbox", { name: "Sleep idle sessions when the Mac runs short of memory" });

  it("sleeps idle sessions after 30 minutes by default, offers never, and saves the minutes", async () => {
    render(<GeneralTab toast={vi.fn()} />);
    const sel = await sleepSelect();
    await waitFor(() => expect(sel).toHaveValue("30"));
    expect(within(sel).getAllByRole("option").map((o) => o.textContent)).toContain("never");
    await userEvent.selectOptions(sel, "0");
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { [SETTING_SLEEP_AFTER]: 0 } }));
    await userEvent.selectOptions(sel, "60");
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { [SETTING_SLEEP_AFTER]: 60 } }));
  });

  it("shows a stored value, never included", async () => {
    state.fx.settings[SETTING_SLEEP_AFTER] = 0;
    render(<GeneralTab toast={vi.fn()} />);
    await waitFor(async () => expect(await sleepSelect()).toHaveValue("0"));
  });

  it("the memory guard is on by default and can be turned off", async () => {
    render(<GeneralTab toast={vi.fn()} />);
    expect(await guardBox()).toBeChecked();
    await userEvent.click(await guardBox());
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { [SETTING_MEMORY_GUARD]: false } }));
    expect(screen.getByText(/never stops a working session: if every session is busy, it only warns/)).toBeInTheDocument();
  });
});
