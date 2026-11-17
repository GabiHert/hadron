import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import App from "./App";
import { FeedbackTab } from "./components/FeedbackTab";
import type { Feedback, SharedFeedback } from "./api";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
const openURL = vi.fn();
vi.mock("../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: (...a: unknown[]) => openURL(...a), EventsOn: () => () => {} }));

const item = (over: Partial<Feedback> = {}): Feedback => ({
  id: "f1", created_at: "2026-11-08T12:00:00Z", kind: "bug", text: "Billing-fix card shows twice on the board", area: "board", version: "v1.2.3",
  diagnostics: null, status: "new", note: "", github_url: "", shared: false, ...over,
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

  it("updates status, note and issue link", async () => {
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
  });

  it("opens a shared note on GitHub instead of offering to send it again", async () => {
    mocks.feedback_list.mockResolvedValue([item({ github_url: "https://github.com/GabiHert/hadron/issues/7", shared: true })]);
    render(<FeedbackTab toast={vi.fn()} />);
    await userEvent.click(await screen.findByRole("button", { name: /Open on GitHub/ }));
    expect(openURL).toHaveBeenCalledWith("https://github.com/GabiHert/hadron/issues/7");
    expect(screen.queryByRole("button", { name: /Send to GitHub/ })).not.toBeInTheDocument();
  });
});

describe("Send to GitHub", () => {
  const openReview = async (toast = vi.fn()) => {
    mocks.feedback_list.mockResolvedValue([item()]);
    render(<FeedbackTab toast={toast} />);
    await userEvent.click(await screen.findByRole("button", { name: /Send to GitHub/ }));
    return screen.findByRole("dialog", { name: "Send to GitHub" });
  };

  it("shows the scrubbed draft for review, sends the edited text, and never sends on its own", async () => {
    const toast = vi.fn();
    const dlg = await openReview(toast);
    await waitFor(() => expect(within(dlg).getByLabelText("Title")).toHaveValue("[bug] A note"));
    expect(mocks.feedback_send).toHaveBeenCalledTimes(1);
    expect(mocks.feedback_send).toHaveBeenCalledWith({ id: "f1", preview: true, keep_details: false });
    expect(within(dlg).getByLabelText("Body")).toHaveValue("At <path>\n\n---\nKind: bug");
    expect(within(dlg).getByText(/Replaced to keep them private: paths/)).toBeInTheDocument();
    expect(within(dlg).getByText(/Opens a filled-in issue in your browser/)).toBeInTheDocument();

    await userEvent.click(within(dlg).getByRole("checkbox", { name: /Keep folder paths/ }));
    await waitFor(() => expect(within(dlg).getByLabelText("Body")).toHaveValue("At /Users/jdoe/x\n\n---\nKind: bug"));

    const title = within(dlg).getByLabelText("Title");
    await userEvent.clear(title);
    await userEvent.type(title, "[[bug] Edited");
    await userEvent.click(within(dlg).getByRole("button", { name: "Open in browser" }));
    await waitFor(() => expect(mocks.feedback_send).toHaveBeenLastCalledWith({ id: "f1", title: "[bug] Edited", body: "At /Users/jdoe/x\n\n---\nKind: bug" }));
    expect(toast).toHaveBeenCalledWith(expect.stringMatching(/Press Submit there/));
    expect(screen.queryByRole("dialog", { name: "Send to GitHub" })).not.toBeInTheDocument();
  });

  it("blocks sending while the text has a private word", async () => {
    mocks.feedback_sharing_get.mockResolvedValueOnce({ repo: "GabiHert/hadron", has_token: true, private_words: ["Acme"] });
    const dlg = await openReview();
    const body = within(dlg).getByLabelText("Body");
    await waitFor(() => expect(body).toHaveValue("At <path>\n\n---\nKind: bug"));
    await userEvent.type(body, " for acme");
    expect(within(dlg).getByRole("alert")).toHaveTextContent("words you keep private: Acme");
    expect(within(dlg).getByRole("button", { name: "Open in browser" })).toBeDisabled();
    await userEvent.type(body, "{Backspace}{Backspace}{Backspace}{Backspace}");
    expect(within(dlg).queryByRole("alert")).not.toBeInTheDocument();
    expect(within(dlg).getByRole("button", { name: "Open in browser" })).toBeEnabled();
  });

  it("saves the private words and the token", async () => {
    const toast = vi.fn();
    render(<FeedbackTab toast={toast} />);
    await userEvent.click(await screen.findByText("Sharing on GitHub"));
    await userEvent.type(screen.getByLabelText(/Words to keep private/), "Acme{Enter}Beta");
    await userEvent.tab();
    await waitFor(() => expect(mocks.feedback_sharing_set).toHaveBeenCalledWith({ private_words: ["Acme", "Beta"] }));
    await userEvent.type(screen.getByLabelText("Feedback token"), "github_pat_x");
    await userEvent.click(screen.getByRole("button", { name: "Save token" }));
    await waitFor(() => expect(mocks.set_feedback_token).toHaveBeenCalledWith({ token: "github_pat_x" }));
    expect(await screen.findByText(/A token is saved/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(mocks.set_feedback_token).toHaveBeenLastCalledWith({ token: "" }));
  });
});

describe("All users", () => {
  const shared = (over: Partial<SharedFeedback> = {}): SharedFeedback => ({
    number: 12, title: "[idea] Pin cards", url: "https://github.com/GabiHert/hadron/issues/12", kind: "idea", version: "v0.2.30", area: "board", text: "Pin cards", created_at: "2026-11-10T10:00:00Z", ...over,
  });

  it("refreshes on open, links each issue, and imports one", async () => {
    mocks.feedback_list_shared.mockResolvedValue({ issues: [shared(), shared({ number: 13, title: "[bug] Mine", local_id: "f9", kind: "bug" })], fetched_at: "2026-11-17T10:00:00Z" });
    const toast = vi.fn();
    render(<FeedbackTab toast={toast} />);
    await userEvent.click(screen.getByRole("tab", { name: "All users" }));
    expect(await screen.findByText("[idea] Pin cards")).toBeInTheDocument();
    expect(mocks.feedback_list_shared).toHaveBeenCalledWith({ refresh: true });
    expect(screen.getAllByText("v0.2.30")).toHaveLength(2);
    expect(screen.getByText("In your inbox")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Open #12 on GitHub" }));
    expect(openURL).toHaveBeenCalledWith("https://github.com/GabiHert/hadron/issues/12");
    await userEvent.click(screen.getByRole("button", { name: "Import #12" }));
    await waitFor(() => expect(mocks.feedback_import_shared).toHaveBeenCalledWith({ number: 12 }));
    expect(toast).toHaveBeenCalledWith("#12 is in your inbox.");
    expect(screen.queryByRole("button", { name: "Import #13" })).not.toBeInTheDocument();
  });

  it("shows why a fetch failed", async () => {
    mocks.feedback_list_shared.mockResolvedValue({ issues: [], fetched_at: "", error: "GitHub's limit for reading without a token was reached" });
    render(<FeedbackTab toast={vi.fn()} />);
    await userEvent.click(screen.getByRole("tab", { name: "All users" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("limit for reading");
  });
});
