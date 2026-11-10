import { render, screen, waitFor, within, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { widgetSrcDoc } from "./components/Widget";
import { termTheme } from "./components/Terminal";
import { mocks, reset, themes } from "./test/fakeApi";
import { resolve } from "./theme";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const root = () => document.documentElement;

async function openAppearance() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Appearance" }));
  await screen.findByRole("heading", { name: "Appearance" });
  await screen.findByRole("button", { name: "Use Nucleus" });
  return user;
}

describe("themes", () => {
  it("resolves System mode to the light or the dark slot", () => {
    const list = { themes: themes.themes, groups: [], active: { theme: "system", light: "nucleus", dark: "deep-space" } };
    expect(resolve(list, false)?.id).toBe("nucleus");
    expect(resolve(list, true)?.id).toBe("deep-space");
    expect(resolve({ ...list, active: { ...list.active, theme: "classic-dark" } }, false)?.id).toBe("classic-dark");
    expect(resolve({ ...list, active: { ...list.active, light: "gone" } }, false)?.id).toBe("classic-light");
  });

  it("applies the theme's tokens to the app, the widgets and the terminal", async () => {
    themes.active = { theme: "deep-space", light: "classic-light", dark: "classic-dark" };
    render(<App />);
    await waitFor(() => expect(root().style.getPropertyValue("--bg")).toBe("#0b0e17"));
    expect(root().getAttribute("data-theme")).toBe("dark");
    expect(widgetSrcDoc("<p>x</p>", { scripts: false })).toContain("--bg:#0b0e17;");
    expect(termTheme()).toMatchObject({ background: "#101010", foreground: "#fefefe" });
    expect(JSON.parse(localStorage.getItem("hadron.theme") ?? "{}").tokens.bg).toBe("#0b0e17");
  });

  it("picks a theme for its mode in System mode, and one theme when not following macOS", async () => {
    const user = await openAppearance();
    expect(screen.getByRole("checkbox", { name: "Follow macOS (light and dark)" })).toBeChecked();
    await user.click(screen.getByRole("button", { name: "Use Nucleus" }));
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ light: "nucleus" }));
    await waitFor(() => expect(root().style.getPropertyValue("--bg")).toBe("#f7f8fb"));
    await user.click(screen.getByRole("button", { name: "Use Deep Space" }));
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ dark: "deep-space" }));
    expect(screen.getByRole("button", { name: "Use Deep Space" })).toHaveAttribute("aria-pressed", "true");

    await user.click(screen.getByRole("checkbox", { name: "Follow macOS (light and dark)" }));
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ theme: "nucleus" }));
    await user.click(screen.getByRole("button", { name: "Use Deep Space" }));
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ theme: "deep-space" }));
    await waitFor(() => expect(root().getAttribute("data-theme")).toBe("dark"));
  });

  it("imports a VS Code theme and switches to it", async () => {
    const user = await openAppearance();
    const json = JSON.stringify({ name: "Acme Night", type: "dark", colors: { "editor.background": "#1a1b26", "editor.foreground": "#c0caf5" } });
    await user.upload(screen.getByLabelText("VS Code theme file"), new File([json], "acme.json", { type: "application/json" }));
    await waitFor(() => expect(mocks.theme_import).toHaveBeenCalledWith({ json }));
    expect(await screen.findByText("Imported Acme Night")).toBeInTheDocument();
    // In System mode a dark theme fills the dark slot; macOS is light here, so it waits for dark.
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ dark: "acme-night" }));
    expect(screen.getByRole("button", { name: "Use Acme Night" })).toHaveAttribute("aria-pressed", "true");
  });

  it("shows the backend's error for a file that is not a theme", async () => {
    const user = await openAppearance();
    await user.upload(screen.getByLabelText("VS Code theme file"), new File(["{}"], "package.json", { type: "application/json" }));
    expect(await screen.findByText("this file is not a VS Code colour theme")).toBeInTheDocument();
  });

  it("duplicates a theme, edits a colour with a live preview, saves it and exports it", async () => {
    const user = await openAppearance();
    await user.click(screen.getByRole("button", { name: "Duplicate Nucleus" }));
    const editor = screen.getByRole("group", { name: "Theme editor" });
    const name = within(editor).getByLabelText("Name");
    expect(name).toHaveValue("Nucleus copy");
    await user.clear(name);
    await user.type(name, "My Lab");
    fireEvent.change(within(editor).getByLabelText("bg hex"), { target: { value: "#abcdef" } });
    expect(within(editor).getByRole("img", { name: "Live preview" }).style.getPropertyValue("--bg")).toBe("#abcdef");
    await user.click(within(editor).getByRole("checkbox", { name: "Try it on the whole app" }));
    await waitFor(() => expect(root().style.getPropertyValue("--bg")).toBe("#abcdef"));
    await user.click(within(editor).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.theme_save).toHaveBeenCalledWith({ theme: expect.objectContaining({ name: "My Lab", mode: "light", tokens: expect.objectContaining({ bg: "#abcdef" }) }) }));
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ light: "my-lab" }));

    await user.click(await screen.findByRole("button", { name: "Export My Lab" }));
    expect(await screen.findByText("Exported My Lab to /Users/me/Downloads/my-lab.hadron-theme.json")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Edit My Lab" }));
    await user.click(screen.getByRole("button", { name: "Delete theme" }));
    await user.click(screen.getByRole("button", { name: "Delete My Lab for good" }));
    await waitFor(() => expect(mocks.theme_delete).toHaveBeenCalledWith({ id: "my-lab" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Use My Lab" })).not.toBeInTheDocument());
  });

  it("cancelling the editor drops the preview", async () => {
    const user = await openAppearance();
    await user.click(screen.getByRole("button", { name: "Duplicate Deep Space" }));
    const editor = screen.getByRole("group", { name: "Theme editor" });
    await user.click(within(editor).getByRole("checkbox", { name: "Try it on the whole app" }));
    await waitFor(() => expect(root().style.getPropertyValue("--bg")).toBe("#0b0e17"));
    await user.click(within(editor).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(root().style.getPropertyValue("--bg")).toBe("#fbfbf8"));
    expect(mocks.theme_save).not.toHaveBeenCalled();
  });

  it("switches theme from the command palette", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await waitFor(() => expect(mocks.theme_list).toHaveBeenCalled());
    fireEvent.keyDown(window, { key: "k", metaKey: true });
    const input = await screen.findByPlaceholderText("Jump to a card, workspace or command");
    await user.type(input, "switch theme deep space{Enter}");
    await waitFor(() => expect(mocks.theme_set).toHaveBeenCalledWith({ dark: "deep-space" }));
  });
});
