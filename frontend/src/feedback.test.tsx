import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import App from "./App";
import { FeedbackTab } from "./components/FeedbackTab";
import type { Feedback } from "./api";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
const openURL = vi.fn();
vi.mock("../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: (...a: unknown[]) => openURL(...a), EventsOn: () => () => {} }));

const item = (over: Partial<Feedback> = {}): Feedback => ({
  id: "f1", created_at: "2026-11-08T12:00:00Z", kind: "bug", text: "Billing-fix card shows twice on the board", area: "board", version: "v1.2.3",
  diagnostics: null, status: "new", note: "", github_url: "", issue_url: "https://github.com/GabiHert/hadron/issues/new?title=x", ...over,
});

beforeEach(() => {
  reset();
  openURL.mockReset();
});

const openModal = async () => {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await userEvent.click(screen.getByRole("button", { name: "Send feedback" }));
  return screen.findByRole("dialog", { name: "Send feedback" });
};

describe("Feedback modal", () => {
  it("opens from the header with Idea selected and Send disabled", async () => {
    const dlg = await openModal();
    const group = within(dlg).getByRole("radiogroup", { name: "Kind" });
    expect(within(group).getAllByRole("button").map((b) => b.textContent)).toEqual(["Bug", "Idea", "Annoyance", "Praise"]);
    expect(within(group).getByRole("button", { name: "Idea" })).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(within(group).getByRole("button", { name: "Bug" }));
    expect(within(group).getByRole("button", { name: "Bug" })).toHaveAttribute("aria-pressed", "true");
    expect(within(group).getByRole("button", { name: "Idea" })).toHaveAttribute("aria-pressed", "false");
    expect(within(dlg).getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("sends kind, text, area and attach_diagnostics, then toasts", async () => {
    const dlg = await openModal();
    await userEvent.click(within(dlg).getByRole("button", { name: "Praise" }));
    await userEvent.type(within(dlg).getByLabelText("What do you want to tell us?"), "Love it");
    await userEvent.click(within(dlg).getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.feedback_create).toHaveBeenCalledWith({ kind: "praise", text: "Love it", area: "board", attach_diagnostics: false }));
    expect(await screen.findByText("Thanks. It's in Settings → Feedback.")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "Send feedback" })).not.toBeInTheDocument();
  });

  it("keeps the modal and text open on error", async () => {
    mocks.feedback_create.mockRejectedValueOnce(new Error("disk full"));
    const dlg = await openModal();
    await userEvent.type(within(dlg).getByLabelText("What do you want to tell us?"), "Hello");
    await userEvent.click(within(dlg).getByRole("button", { name: "Send" }));
    expect(await screen.findByText("disk full")).toBeInTheDocument();
    expect(screen.getByLabelText("What do you want to tell us?")).toHaveValue("Hello");
  });

  it("attaches diagnostics by default when debug logging is on", async () => {
    state.fx.settings["debug.logging"] = true;
    const dlg = await openModal();
    const box = within(dlg).getByRole("checkbox", { name: /Attach diagnostics/ });
    await waitFor(() => expect(box).toBeChecked());
    await userEvent.type(within(dlg).getByLabelText("What do you want to tell us?"), "x");
    await userEvent.click(within(dlg).getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.feedback_create).toHaveBeenCalledWith(expect.objectContaining({ attach_diagnostics: true })));
  });

  it("leaves attach off when debug logging is off", async () => {
    const dlg = await openModal();
    expect(within(dlg).getByRole("checkbox", { name: /Attach diagnostics/ })).not.toBeChecked();
  });

  it("uses a settings area when opened from Settings", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await userEvent.click(screen.getByRole("button", { name: "Settings" }));
    await userEvent.click(await screen.findByRole("button", { name: "Feedback" }));
    await userEvent.click(screen.getAllByRole("button", { name: "Send feedback" })[0]);
    const dlg = await screen.findByRole("dialog", { name: "Send feedback" });
    await userEvent.type(within(dlg).getByLabelText("What do you want to tell us?"), "Hi");
    await userEvent.click(within(dlg).getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.feedback_create).toHaveBeenCalledWith(expect.objectContaining({ area: "settings:feedback" })));
  });
});

describe("Feedback tab", () => {
  it("shows the empty state", async () => {
    render(<FeedbackTab toast={vi.fn()} />);
    expect(await screen.findByText(/No feedback yet/)).toBeInTheDocument();
  });

  it("lists items newest first and filters by status", async () => {
    mocks.feedback_list.mockResolvedValue([item({ id: "a", text: "Older one", created_at: "2026-11-01T10:00:00Z" }), item({ id: "b", text: "Newer one", kind: "praise" })]);
    render(<FeedbackTab toast={vi.fn()} />);
    const texts = await screen.findAllByText(/(Older|Newer) one/);
    expect(texts.map((t) => t.textContent)).toEqual(["Newer one", "Older one"]);
    expect(screen.getByText("praise")).toHaveClass("ok");
    await userEvent.selectOptions(screen.getByLabelText("Status"), "planned");
    await waitFor(() => expect(mocks.feedback_list).toHaveBeenLastCalledWith({ status: "planned" }));
  });

  it("updates status, note and issue link, and opens the issue", async () => {
    mocks.feedback_list.mockResolvedValue([item()]);
    render(<FeedbackTab toast={vi.fn()} />);
    await userEvent.selectOptions(await screen.findByLabelText("Status of Billing-fix card sho"), "done");
    await waitFor(() => expect(mocks.feedback_update).toHaveBeenCalledWith({ id: "f1", status: "done" }));
    const note = screen.getByLabelText(/Note for/);
    await userEvent.type(note, "looked at it");
    await userEvent.tab();
    await waitFor(() => expect(mocks.feedback_update).toHaveBeenCalledWith({ id: "f1", note: "looked at it" }));
    const link = screen.getByLabelText(/Issue link for/);
    await userEvent.type(link, "https://github.com/Acme/app/issues/1");
    await userEvent.tab();
    await waitFor(() => expect(mocks.feedback_update).toHaveBeenCalledWith({ id: "f1", github_url: "https://github.com/Acme/app/issues/1" }));
    await userEvent.click(screen.getByRole("button", { name: /Open as GitHub issue/ }));
    expect(openURL).toHaveBeenCalledWith("https://github.com/GabiHert/hadron/issues/new?title=x");
  });
});
