import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { QuestionCard } from "./components/Chat";
import { question } from "./test/fakeApi";

const mcp = () =>
  question({
    kind: "approval",
    text: "New MCP server found in this project: acme",
    context: "MCP servers may execute code or access system resources.",
    approval: { target: "/work/acme", runs_as: "Claude Code in billing-fix", reversible: "partial", reversible_note: "The choice is made once.", command: "MCP servers may execute code", command_lang: "text" },
    options: [
      { label: "Use this MCP server", description: "" },
      { label: "Use this and all future MCP servers in this project", description: "" },
      { label: "Continue without using this MCP server", description: "", default: true },
      { label: "Cancel (Esc)", description: "Close the menu without choosing" },
    ],
    menu: { sig: "abc", kind: "mcp" },
  });

describe("a question for Claude Code's menu in the Terminal", () => {
  it("says where it comes from and preselects the highlighted row", async () => {
    const user = userEvent.setup();
    const onAnswer = vi.fn();
    render(<QuestionCard q={mcp()} onAnswer={onAnswer} />);
    expect(screen.getByText(/Claude Code menu in the Terminal · MCP server/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Continue without/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: /^1\s*Use this MCP server/ })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getAllByTestId("menu-default")).toHaveLength(1);
    expect(screen.getByText(/highlighted in the Terminal/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^1\s*Use this MCP server/ }));
    await user.click(screen.getByRole("button", { name: "Answer" }));
    expect(onAnswer).toHaveBeenCalledWith(["Use this MCP server"], "");
  });

  it("needs a row when the menu has nowhere to type, and says notes go to Claude after it", async () => {
    const user = userEvent.setup();
    const q = mcp();
    q.options = q.options.map((o) => ({ ...o, default: false }));
    render(<QuestionCard q={q} onAnswer={vi.fn()} />);
    await user.type(screen.getByLabelText(/Notes/), "not now");
    expect(screen.getByRole("button", { name: "Answer" })).toBeDisabled();
    expect(screen.getByText(/sent to Claude after the menu/)).toBeInTheDocument();
  });

  it("takes an answer in your own words when the menu has a free-text row, and ticks the ticked rows", async () => {
    const user = userEvent.setup();
    const onAnswer = vi.fn();
    const q = question({
      multi: true,
      text: "Which checks should run?",
      options: [
        { label: "Lint", description: "Static checks" },
        { label: "Tests", description: "Unit tests", default: true },
        { label: "Build", description: "Compile the app", default: true },
      ],
      menu: { sig: "def", kind: "question", free_text: true },
    });
    render(<QuestionCard q={q} onAnswer={onAnswer} />);
    expect(screen.getByRole("button", { name: /Tests/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: /Build/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getAllByText(/ticked in the Terminal/)).toHaveLength(2);
    expect(screen.getByText(/typed into the menu/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Tests/ }));
    await user.type(screen.getByLabelText(/Notes/), "and the e2e suite");
    await user.click(screen.getByRole("button", { name: "Answer" }));
    expect(onAnswer).toHaveBeenCalledWith(["Build"], "and the e2e suite");
  });

  it("says when the menu was answered in the Terminal", () => {
    render(<QuestionCard q={{ ...mcp(), status: "answered", answer: { selections: [], notes: "", via: "terminal" } }} onAnswer={vi.fn()} />);
    expect(screen.getByText("Answered in the Terminal.")).toBeInTheDocument();
  });
});
