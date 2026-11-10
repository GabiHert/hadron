import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openNewWorkspace() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "New workspace" }));
  const dialog = await screen.findByRole("dialog", { name: "New workspace" });
  return { user, dialog };
}

describe("New workspace: choosing the root folder", () => {
  it("opens the folder picker at the typed path and fills the field with the choice", async () => {
    mocks.chooseFolder.mockResolvedValueOnce("/Users/you/Projects/acme");
    const { user, dialog } = await openNewWorkspace();
    const root = within(dialog).getByRole("textbox", { name: "Root folder" });
    expect(root).toHaveValue("~/Documents/Projects/");
    await user.click(within(dialog).getByRole("button", { name: "Choose…" }));
    expect(mocks.chooseFolder).toHaveBeenCalledWith("~/Documents/Projects/");
    await waitFor(() => expect(root).toHaveValue("/Users/you/Projects/acme"));

    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Acme two");
    await user.click(within(dialog).getByRole("button", { name: "Create workspace" }));
    await waitFor(() => expect(mocks.workspace_create).toHaveBeenCalled());
    expect(mocks.workspace_create.mock.calls[0][0]).toMatchObject({ name: "Acme two", root: "/Users/you/Projects/acme" });
  });

  it("keeps the typed path when the picker is cancelled, and typing still works", async () => {
    const { user, dialog } = await openNewWorkspace();
    const root = within(dialog).getByRole("textbox", { name: "Root folder" });
    await user.clear(root);
    await user.type(root, "~/code/beta");
    mocks.chooseFolder.mockResolvedValueOnce(""); // cancelled
    await user.click(within(dialog).getByRole("button", { name: "Choose…" }));
    expect(mocks.chooseFolder).toHaveBeenCalledWith("~/code/beta");
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Choose…" })).toBeEnabled());
    expect(root).toHaveValue("~/code/beta");
  });

  it("is reached with Tab from the path field and opens with the keyboard", async () => {
    mocks.chooseFolder.mockResolvedValueOnce("/Users/you/Projects/beta");
    const { user, dialog } = await openNewWorkspace();
    const root = within(dialog).getByRole("textbox", { name: "Root folder" });
    root.focus();
    await user.tab();
    expect(within(dialog).getByRole("button", { name: "Choose…" })).toHaveFocus();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(root).toHaveValue("/Users/you/Projects/beta"));
  });

  it("shows the picker's error and leaves the field as it was", async () => {
    mocks.chooseFolder.mockRejectedValueOnce(new Error("the window is not ready yet; try again in a moment"));
    const { user, dialog } = await openNewWorkspace();
    await user.click(within(dialog).getByRole("button", { name: "Choose…" }));
    expect(await screen.findByText(/the window is not ready yet/)).toBeInTheDocument();
    expect(within(dialog).getByRole("textbox", { name: "Root folder" })).toHaveValue("~/Documents/Projects/");
  });
});
