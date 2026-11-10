import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset, state } from "./test/fakeApi";
import { autoColor, keyPrefix, resetChipColorsForTests, tagColor, TAG_COLORS } from "./state/chipColors";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => {
  reset();
  resetChipColorsForTests();
  state.chips = { c2: [{ path: "jira.status", label: "Jira status", value: "Blocked" }] };
  state.fx.cards.w1[1].stage = "review";
});

async function openApp() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  return user;
}
const face = (nick: string) => screen.getByRole("button", { name: `Open ${nick}` });

describe("tag colours", () => {
  it("picks a stable automatic colour, never grey, and lets a rule or the plain switch win", () => {
    expect(autoColor("In Progress")).toBe(autoColor("In Progress"));
    for (const v of ["a", "b", "Blocked", "Done", "review", "opus"]) expect(autoColor(v)).not.toBe("grey");
    expect(new Set(["To Do", "In Progress", "Blocked", "Done", "review", "qa", "opus", "sonnet"].map(autoColor)).size).toBeGreaterThan(2);
    const cc = { palette: TAG_COLORS, rules: [{ path: "jira.status", value: "Blocked", color: "red" as const }, { path: "card.key", value: "ACME", color: "purple" as const }], plain: ["card.model"] };
    expect(tagColor(cc, "jira.status", "Blocked")).toBe("red");
    expect(tagColor(cc, "jira.status", "Done")).toBe(autoColor("Done"));
    expect(tagColor(cc, "card.model", "opus")).toBeNull();
    expect(tagColor(cc, "card.key", "ACME-508")).toBe("purple");
    expect(tagColor(cc, "card.key", "BETA-1")).toBeNull();
    expect(tagColor(cc, "card.stage", "")).toBeNull();
    expect(keyPrefix("acme-508")).toBe("ACME");
  });

  it("colours the card face: field chips, the stage and the key prefix", async () => {
    state.chipColors = { palette: TAG_COLORS, rules: [{ path: "jira.status", value: "Blocked", color: "red" }, { path: "card.key", value: "ACME", color: "purple" }], plain: [] };
    await openApp();
    const card = face("api-timeouts");
    await waitFor(() => expect(within(card).getByText("Blocked").className).toContain("tag-red"));
    expect(within(card).getByText("review").className).toContain(`tag-${autoColor("review")}`);
    expect(within(card).getByText("ACME-541").className).toContain("tag-purple");
  });

  it("sets a colour from the card panel and turns a field's automatic colours off", async () => {
    const user = await openApp();
    await user.click(face("api-timeouts"));
    const row = await screen.findByLabelText("Card tags");
    await user.click(within(row).getByRole("button", { name: "Colour of Jira status Blocked" }));
    const picker = screen.getByRole("dialog", { name: "Colour for Jira status Blocked" });
    await user.click(within(picker).getByRole("button", { name: "blue" }));
    await waitFor(() => expect(mocks.chip_color_set).toHaveBeenCalledWith({ workspace_id: "w1", path: "jira.status", value: "Blocked", color: "blue" }));
    await waitFor(() => expect(within(face("api-timeouts")).getByText("Blocked").className).toContain("tag-blue"));

    await user.click(within(row).getByRole("button", { name: "Colour of Stage review" }));
    await user.click(screen.getByRole("checkbox", { name: /Automatic colours for every Stage value/ }));
    await waitFor(() => expect(mocks.chip_color_auto_set).toHaveBeenCalledWith({ workspace_id: "w1", path: "card.stage", auto: false }));
    await waitFor(() => expect(within(face("api-timeouts")).getByText("review").className).toBe("pill"));
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: /Colour for Stage/ })).not.toBeInTheDocument();
  });

  it("opens a tag's picker with Enter from the keyboard", async () => {
    const user = await openApp();
    await user.click(face("api-timeouts"));
    const tag = within(await screen.findByLabelText("Card tags")).getByRole("button", { name: "Colour of Jira status Blocked" });
    tag.focus();
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("dialog", { name: "Colour for Jira status Blocked" })).toBeInTheDocument();
    expect(document.activeElement?.getAttribute("aria-label")).toBe("grey");
  });

  it("Settings → Tags lists each field's values and its automatic switch", async () => {
    const user = await openApp();
    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Tags" }));
    const status = await screen.findByRole("group", { name: "Tags of Jira status" });
    expect(within(status).getByRole("button", { name: "Colour of Jira status Blocked" })).toBeInTheDocument();
    const keys = screen.getByRole("group", { name: "Tags of Key prefix" });
    expect(within(keys).getByRole("button", { name: "Colour of Key prefix ACME" })).toBeInTheDocument();
    expect(within(keys).queryByRole("switch")).not.toBeInTheDocument();
    await user.click(within(status).getByRole("switch", { name: "Automatic colours for Jira status" }));
    await waitFor(() => expect(mocks.chip_color_auto_set).toHaveBeenCalledWith({ workspace_id: "w1", path: "jira.status", auto: false }));
  });
});
