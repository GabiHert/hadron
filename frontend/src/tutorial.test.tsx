import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { captionPlace, CHAPTER_STING, chapterStart, CHAPTERS, MOCK_SOUNDS, HANDOFF_EXAMPLES, HANDOFF_PROMPT, showTutorial, targetBox, TOUR_STEPS, Tutorial } from "./components/Tutorial";
import { isFullScreen } from "./components/Header";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const press = (key: string, init: Partial<KeyboardEventInit> & { code?: string } = {}) => fireEvent.keyDown(document.activeElement ?? window, { key, ...init });
const tour = () => screen.findByRole("dialog", { name: /./, hidden: false });

async function firstLaunch() {
  delete state.fx.settings["tutorial.seen"];
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  return screen.findByRole("dialog", { name: "Welcome to Hadron" });
}

describe("the tutorial", () => {
  it("opens on a first launch, ahead of What's new, and moves with ← → ", async () => {
    state.changelogShow = true;
    const dlg = await firstLaunch();
    expect(within(dlg).getByText(/Basics · 1 of 5/)).toBeInTheDocument();
    expect(within(dlg).getByText("(step 1 of 15)")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: /What's new/i })).not.toBeInTheDocument();
    await waitFor(() => expect(within(dlg).getByRole("button", { name: "Next →" })).toHaveFocus());
    press("ArrowRight");
    expect(await screen.findByRole("dialog", { name: "Workspaces" })).toBeInTheDocument();
    press("ArrowLeft");
    expect(await screen.findByRole("dialog", { name: "Welcome to Hadron" })).toBeInTheDocument();
    press("ArrowLeft");
    expect(screen.getByText("(step 1 of 15)")).toBeInTheDocument();
  });

  it("Esc skips it and remembers it was seen", async () => {
    await firstLaunch();
    press("Escape");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Welcome to Hadron" })).not.toBeInTheDocument());
    expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "tutorial.seen": true } });
  });

  it("walks all fifteen steps with Next, with a mock where the element is missing, and creates nothing", async () => {
    const user = userEvent.setup();
    const dlg = await firstLaunch();
    expect(dlg.querySelector(".tour-ill")).not.toBeNull();
    const titles: string[] = [];
    for (let i = 0; i < TOUR_STEPS.length; i++) {
      const cur = await tour();
      titles.push(within(cur).getByRole("heading").textContent ?? "");
      expect(within(cur).getByText(`(step ${i + 1} of 15)`)).toBeInTheDocument();
      if (TOUR_STEPS[i].target) {
        // jsdom lays nothing out, so every element counts as missing: the step shows its mock.
        expect(within(cur).getByText(/A preview/)).toBeInTheDocument();
        expect(cur.querySelector(".tour-ill.big")).not.toBeNull();
      }
      await user.click(within(cur).getByRole("button", { name: i === TOUR_STEPS.length - 1 ? "Ask the planner →" : "Next →" }));
    }
    expect(titles).toEqual(TOUR_STEPS.map((s) => s.title));
    await waitFor(() => expect(document.querySelector(".tour")).toBeNull());
    expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "tutorial.seen": true } });
    expect(mocks.card_create).not.toHaveBeenCalled();
    expect(mocks.workspace_create).not.toHaveBeenCalled();
  });

  it("stays closed once seen, and replays from ⌘K and Settings", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await act(async () => void (await new Promise((r) => setTimeout(r, 30))));
    expect(document.querySelector(".tour")).toBeNull();
    expect(mocks.onEvent).toHaveBeenCalledWith("tutorial:show", expect.any(Function));

    press("k", { metaKey: true, code: "KeyK" });
    const input = await screen.findByPlaceholderText("Jump to a card, workspace or command");
    fireEvent.change(input, { target: { value: "show the tutorial" } });
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("Show the tutorial");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(await screen.findByRole("dialog", { name: "Welcome to Hadron" })).toBeInTheDocument();
    press("Escape");
    await waitFor(() => expect(document.querySelector(".tour")).toBeNull());

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "General" }));
    await user.click(await screen.findByRole("button", { name: "Replay tutorial" }));
    expect(await screen.findByRole("dialog", { name: "Welcome to Hadron" })).toBeInTheDocument();
  });

  it("spotlights the real element when it is on screen", async () => {
    document.body.innerHTML = '<div data-tour="workspaces" id="ws"></div>';
    const el = document.getElementById("ws") as HTMLElement;
    el.getBoundingClientRect = () => ({ top: 10, left: 100, width: 300, height: 34, right: 400, bottom: 44, x: 100, y: 10, toJSON: () => ({}) }) as DOMRect;
    expect(targetBox('[data-tour="workspaces"]')).toEqual({ top: 4, left: 94, width: 312, height: 46 });
    expect(targetBox('[data-tour="nope"]')).toBeNull();
    render(<Tutorial onClose={() => {}} start={1} />);
    const dlg = await screen.findByRole("dialog", { name: "Workspaces" });
    expect(within(dlg).queryByText(/A preview/)).toBeNull();
    const hole = document.querySelector(".tour-hole") as HTMLElement;
    expect(hole.classList.contains("none")).toBe(false);
    expect(hole.style.left).toBe("94px");
    expect(hole.style.width).toBe("312px");
  });

  it("has four chapters, jumps between them, and shows the real MCP address", async () => {
    expect(CHAPTERS.map((c) => c.title)).toEqual(["Basics", "Working with the planner", "Automate", "MCP"]);
    expect(TOUR_STEPS.length).toBe(15);
    expect(TOUR_STEPS.filter((s) => s.chapter === "mcp").map((s) => s.id)).toEqual(["mcp-tools", "mcp-connect", "mcp-live", "handoff"]);
    for (const id of ["integrations", "routines", "widgets", "dashboard"]) expect(TOUR_STEPS.some((s) => s.id === id)).toBe(true);
    expect(chapterStart("automate")).toBe(8);
    expect(chapterStart("nope")).toBe(0);

    const user = userEvent.setup();
    render(<Tutorial onClose={() => {}} mcp={{ url: "http://127.0.0.1:47820/mcp", tools: 61 }} />);
    const chapters = screen.getByRole("navigation", { name: "Tutorial chapters" });
    expect(within(chapters).getByRole("button", { name: "Basics" })).toHaveAttribute("aria-current", "step");
    await user.click(within(chapters).getByRole("button", { name: "MCP" }));
    const dlg = await screen.findByRole("dialog", { name: "Everything is an MCP tool" });
    expect(within(dlg).getByText(/MCP · 1 of 4/)).toBeInTheDocument();
    expect(dlg.textContent).toContain("card_create");
    press("ArrowRight");
    const connect = await screen.findByRole("dialog", { name: "Connect a session" });
    expect(connect.textContent).toContain("http://127.0.0.1:47820/mcp");
    expect(connect.textContent).toContain("61 tools");
    expect(connect.textContent).not.toContain("tok-");
    press("ArrowRight");
    const live = await screen.findByRole("dialog", { name: "Watch it work" });
    expect(live.textContent).toContain("hadron_guide");
    expect(within(live).getByRole("button", { name: "Next →" })).toBeInTheDocument();
  });

  it("opens at a chapter from Settings and ⌘K, with the real server address", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    press("k", { metaKey: true, code: "KeyK" });
    const input = await screen.findByPlaceholderText("Jump to a card, workspace or command");
    fireEvent.change(input, { target: { value: "tutorial mcp" } });
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("Tutorial: MCP");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(await screen.findByRole("dialog", { name: "Everything is an MCP tool" })).toBeInTheDocument();
    press("ArrowRight");
    expect((await screen.findByRole("dialog", { name: "Connect a session" })).textContent).toContain("61 tools");
    press("Escape");
    await waitFor(() => expect(document.querySelector(".tour")).toBeNull());

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "General" }));
    await user.click(await screen.findByRole("button", { name: "Tutorial chapter: Automate" }));
    expect(await screen.findByRole("dialog", { name: "The dashboard" })).toBeInTheDocument();
  });

  it("ends by handing over to the planner: the prompt waits in its message box, not sent", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    act(() => showTutorial("mcp"));
    await user.click(await screen.findByRole("button", { name: "Next →" }));
    await user.click(await screen.findByRole("button", { name: "Next →" }));
    await user.click(await screen.findByRole("button", { name: "Next →" }));
    const last = await screen.findByRole("dialog", { name: "Let the planner set it up" });
    expect(last.textContent).toMatch(/workspaces, columns, cards, integrations, routines, rules, dashboards, card groups and themes/);
    const chips = within(last).getByRole("group", { name: "Example prompts for the planner" });
    expect(within(chips).getAllByRole("button")).toHaveLength(3);
    await user.click(within(last).getByRole("button", { name: "Ask the planner →" }));
    await waitFor(() => expect(document.querySelector(".tour")).toBeNull());
    const box = screen.getByLabelText("Message the planner") as HTMLTextAreaElement;
    await waitFor(() => expect(box.value).toBe(HANDOFF_PROMPT));
    expect(mocks.chat_send).not.toHaveBeenCalled();
    expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "tutorial.seen": true } });

    act(() => showTutorial("mcp"));
    for (let k = 0; k < 3; k++) await user.click(await screen.findByRole("button", { name: "Next →" }));
    await user.click(await screen.findByRole("button", { name: HANDOFF_EXAMPLES[1] }));
    await waitFor(() => expect((screen.getByLabelText("Message the planner") as HTMLTextAreaElement).value).toBe(HANDOFF_EXAMPLES[1]));
  });

  it("asks for a workspace first when there is none, and keeps the prompt for the planner", async () => {
    const user = userEvent.setup();
    state.fx.workspaces = [];
    state.fx.settings.active_workspace = "";
    render(<App />);
    act(() => showTutorial("mcp"));
    for (let k = 0; k < 3; k++) await user.click(await screen.findByRole("button", { name: "Next →" }));
    await user.click(await screen.findByRole("button", { name: "Ask the planner →" }));
    expect(await screen.findByRole("dialog", { name: /New workspace/i })).toBeInTheDocument();
    expect(await screen.findByText(/your message to the planner will be waiting/)).toBeInTheDocument();
  });

  it("plays soft sounds through Hadron's player, and its mute is remembered", async () => {
    const user = userEvent.setup();
    const play = vi.fn();
    const onMute = vi.fn();
    const { rerender } = render(<Tutorial onClose={() => {}} play={play} onMute={onMute} />);
    await screen.findByRole("dialog", { name: "Welcome to Hadron" });
    await waitFor(() => expect(play).toHaveBeenCalledWith("tour_ch_basics"));
    press("ArrowRight");
    await screen.findByRole("dialog", { name: "Workspaces" });
    await waitFor(() => expect(play).toHaveBeenLastCalledWith("tour_tick"));
    await user.click(screen.getByRole("button", { name: "Mute the tutorial's sounds" }));
    expect(onMute).toHaveBeenCalledWith(true);
    rerender(<Tutorial onClose={() => {}} play={play} onMute={onMute} muted />);
    const n = play.mock.calls.length;
    press("ArrowRight");
    await screen.findByRole("dialog", { name: "The board" });
    expect(play).toHaveBeenCalledTimes(n);
    expect(screen.getByRole("button", { name: "Turn the tutorial's sounds on" })).toHaveAttribute("aria-pressed", "true");

    const chime = vi.fn();
    render(<Tutorial onClose={() => {}} play={chime} start={TOUR_STEPS.length - 1} />);
    await user.click(screen.getAllByRole("button", { name: "Ask the planner →" }).at(-1)!);
    expect(chime).toHaveBeenCalledWith("tour_chime");
  });

  it("each mock has its own sounds, timed to its animation, and Reduce motion leaves them out", async () => {
    vi.useFakeTimers();
    try {
      const play = vi.fn();
      render(<Tutorial onClose={() => {}} play={play} start={chapterStart("basics") + 2} />);
      expect(screen.getByRole("dialog", { name: "The board" })).toBeInTheDocument();
      act(() => void vi.advanceTimersByTime(700));
      expect(play).toHaveBeenCalledWith("tour_swish");
      expect(play).not.toHaveBeenCalledWith("tour_thud");
      act(() => void vi.advanceTimersByTime(1200));
      expect(play).toHaveBeenCalledWith("tour_thud");
      // Moving on cancels what the old step still had to play.
      play.mockClear();
      act(() => void press("ArrowRight"));
      act(() => void vi.advanceTimersByTime(5000));
      expect(play.mock.calls.map((c) => c[0])).toEqual(["tour_tick", "tour_pop"]);
    } finally {
      vi.useRealTimers();
    }
    for (const [illus, list] of Object.entries(MOCK_SOUNDS)) {
      expect(TOUR_STEPS.some((s) => s.illus === illus)).toBe(true);
      for (const [, at] of list!) expect(at).toBeLessThan(2600);
    }

    const mm = window.matchMedia;
    window.matchMedia = ((q: string) => ({ matches: q.includes("reduce"), media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
    try {
      vi.useFakeTimers();
      const play = vi.fn();
      render(<Tutorial onClose={() => {}} play={play} start={chapterStart("basics") + 2} />);
      act(() => void vi.advanceTimersByTime(3000));
      expect(play.mock.calls.map((c) => c[0])).toEqual(["tour_ch_basics"]);
    } finally {
      vi.useRealTimers();
      window.matchMedia = mm;
    }
  });

  it("opens each chapter with its own sting", async () => {
    const user = userEvent.setup();
    const play = vi.fn();
    render(<Tutorial onClose={() => {}} play={play} />);
    for (const c of CHAPTERS) {
      // The caption is new for each step, so its chapter list is too.
      await user.click(within(screen.getByRole("navigation", { name: "Tutorial chapters" })).getByRole("button", { name: c.title }));
      await waitFor(() => expect(play).toHaveBeenCalledWith(CHAPTER_STING[c.id]));
    }
    expect(new Set(Object.values(CHAPTER_STING)).size).toBe(4);
  });

  it("clicks softly on hover and press, and switches the ambient pad", async () => {
    const user = userEvent.setup();
    const ui = vi.fn();
    const onAmbient = vi.fn();
    const { rerender } = render(<Tutorial onClose={() => {}} ui={ui} ambient onAmbient={onAmbient} />);
    const next = screen.getByRole("button", { name: "Next →" });
    fireEvent.pointerOver(next);
    fireEvent.pointerOver(next);
    expect(ui.mock.calls.filter((c) => c[0] === "hover")).toHaveLength(1);
    fireEvent.pointerDown(next);
    expect(ui).toHaveBeenLastCalledWith("press");
    await user.click(screen.getByRole("button", { name: "Turn the ambient music off" }));
    expect(onAmbient).toHaveBeenCalledWith(false);
    ui.mockClear();
    rerender(<Tutorial onClose={() => {}} ui={ui} ambient onAmbient={onAmbient} muted />);
    fireEvent.pointerDown(screen.getByRole("button", { name: "Next →" }));
    expect(ui).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Turn the ambient music off" })).toBeDisabled();
  });

  it("App plays through sound_play and remembers the mute in tutorial.muted", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    act(() => showTutorial());
    await screen.findByRole("dialog", { name: "Welcome to Hadron" });
    await waitFor(() => expect(mocks.sound_play).toHaveBeenCalledWith({ sound: "tour_ch_basics" }));
    await user.click(screen.getByRole("button", { name: "Mute the tutorial's sounds" }));
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "tutorial.muted": true } }));
  });

  it("knows when the window fills the screen, where the window buttons are hidden", () => {
    const screenOf = (w: number, h: number) => ({ width: w, height: h }) as Screen;
    expect(isFullScreen({ innerWidth: 1512, innerHeight: 982, screen: screenOf(1512, 982) })).toBe(true);
    expect(isFullScreen({ innerWidth: 1400, innerHeight: 900, screen: screenOf(1512, 982) })).toBe(false);
    expect(isFullScreen({ innerWidth: 1024, innerHeight: 768, screen: screenOf(0, 0) })).toBe(false);
  });

  it("places the caption below, above, beside or in the middle", () => {
    const view = { w: 1200, h: 800 };
    const cap = { w: 340, h: 200 };
    expect(captionPlace({ top: 10, left: 100, width: 300, height: 40 }, cap, view)).toEqual({ top: 64, left: 80 });
    expect(captionPlace({ top: 700, left: 100, width: 300, height: 60 }, cap, view).top).toBe(486);
    const side = captionPlace({ top: 40, left: 0, width: 340, height: 760 }, cap, view);
    expect(side.left).toBe(354);
    expect(captionPlace(null, cap, view)).toEqual({ top: 300, left: 430 });
  });
});
