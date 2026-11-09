import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { attachmentsIn, deliverPathDrop, shellQuote, typedPaths, withAttachments } from "./attach";
import { ChatLine } from "./components/Chat";
import { Composer } from "./components/Composer";
import { Terminal } from "./components/Terminal";
import { message, mocks, reset } from "./test/fakeApi";
import { FakeXTerm, resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

beforeEach(() => {
  reset();
  resetTerm();
});

const png = (name = "shot.png", size = 16) => new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47, ...new Array(size).fill(0)])], name, { type: "image/png" });
const pdf = (name = "spec.pdf") => new File(["%PDF-1.7\n"], name, { type: "application/pdf" });
const clip = (files: File[], text = "") => ({
  items: files.map((f) => ({ kind: "file", type: f.type, getAsFile: () => f })),
  files,
  types: files.length ? ["Files"] : ["text/plain"],
  getData: (t: string) => (t === "text/plain" ? text : ""),
});
const term = () => FakeXTerm.instances[FakeXTerm.instances.length - 1];
const PATH1 = "/Users/you/.hadron/attachments/2026-11-09/00000000-0000-4000-8000-000000000001.png";

describe("attach helpers", () => {
  it("quotes paths the way Terminal types a dropped file", () => {
    expect(shellQuote("/Users/you/a.png")).toBe("/Users/you/a.png");
    expect(shellQuote("/Users/you/My Shots/it's (1).png")).toBe("/Users/you/My\\ Shots/it\\'s\\ \\(1\\).png");
    expect(typedPaths(["/a b.png", "/c.pdf"])).toBe("/a\\ b.png /c.pdf ");
  });

  it("appends one Attached line per file and finds them again for thumbnails", () => {
    const text = withAttachments("look at this", [PATH1, "/Users/you/.hadron/attachments/2026-11-09/00000000-0000-4000-8000-000000000002.pdf"]);
    expect(text).toBe(`look at this\n\nAttached: ${PATH1}\nAttached: /Users/you/.hadron/attachments/2026-11-09/00000000-0000-4000-8000-000000000002.pdf`);
    expect(attachmentsIn(text)).toEqual([
      { path: PATH1, url: "/attachments/2026-11-09/00000000-0000-4000-8000-000000000001.png", pdf: false },
      { path: "/Users/you/.hadron/attachments/2026-11-09/00000000-0000-4000-8000-000000000002.pdf", url: "", pdf: true },
    ]);
    // A path outside an attachments folder never becomes a thumbnail.
    expect(attachmentsIn("Attached: /etc/passwd and /tmp/x.png")).toEqual([]);
  });
});

describe("terminal attachments", () => {
  it("keeps a pasted image and types its path; text pastes stay xterm's", async () => {
    const view = render(<Terminal sessionId="s1" label="card" />);
    const host = view.container.querySelector(".xterm-host")!;
    fireEvent.paste(host, { clipboardData: clip([png()]) });
    await waitFor(() => expect(term().pasted).toEqual([PATH1 + " "]));
    expect(mocks.attachment_save).toHaveBeenCalledWith({ name: "shot.png", data: expect.any(String) });
    // Text only: the terminal does not touch it.
    fireEvent.paste(host, { clipboardData: clip([], "hello") });
    expect(mocks.attachment_save).toHaveBeenCalledTimes(1);
  });

  it("types the shell-quoted paths of files dropped on it in the app", async () => {
    const view = render(<Terminal sessionId="s1" label="card" />);
    const host = view.container.querySelector(".xterm-host")!;
    act(() => deliverPathDrop(host, ["/Users/you/My Shots/a b.png", "/Users/you/spec.pdf"]));
    expect(term().pasted).toEqual(["/Users/you/My\\ Shots/a\\ b.png /Users/you/spec.pdf "]);
    expect(mocks.attachment_save).not.toHaveBeenCalled();
  });

  it("keeps dropped files first when only their bytes are known (outside the app)", async () => {
    const view = render(<Terminal sessionId="s1" label="card" />);
    const host = view.container.querySelector(".xterm-host")!;
    fireEvent.drop(host, { dataTransfer: clip([png("a.png")]) });
    await waitFor(() => expect(term().pasted).toEqual([PATH1 + " "]));
  });

  it("the Attach button picks files and types their paths; a file over 20 MB is refused", async () => {
    const user = userEvent.setup();
    render(<Terminal sessionId="s1" label="card" />);
    await user.upload(screen.getByLabelText("Choose files to attach to the session"), [png(), pdf()]);
    await waitFor(() => expect(term().pasted).toEqual([`${PATH1} /Users/you/.hadron/attachments/2026-11-09/00000000-0000-4000-8000-000000000002.pdf `]));
    const big = png("big.png");
    Object.defineProperty(big, "size", { value: 21 * 1024 * 1024 });
    await user.upload(screen.getByLabelText("Choose files to attach to the session"), big);
    expect(await screen.findByText("big.png is larger than 20 MB, the most Hadron attaches.")).toBeInTheDocument();
    expect(mocks.attachment_save).toHaveBeenCalledTimes(2);
  });

  it("Option as Meta is a saved setting that the terminal follows", async () => {
    const user = userEvent.setup();
    render(<Terminal sessionId="s1" label="card" />);
    expect(term().options.macOptionIsMeta).toBe(false);
    await user.click(screen.getByRole("checkbox", { name: "Option as Meta" }));
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "terminal.mac_option_is_meta": true } }));
    expect(term().options.macOptionIsMeta).toBe(true);
  });
});

describe("chat attachments", () => {
  it("a pasted image becomes a chip, and Send adds its path to the message", async () => {
    const user = userEvent.setup();
    const onSend = vi.fn();
    render(<Composer id="m" label="Message" placeholder="" onSend={onSend} />);
    const box = screen.getByLabelText("Message");
    fireEvent.paste(box, { clipboardData: clip([png()]) });
    // The row shows "Attaching…" first; wait for the saved chip itself.
    await waitFor(() => expect(screen.getByLabelText("Attachments to send").querySelector("img")).toHaveAttribute("src", "/attachments/2026-11-09/00000000-0000-4000-8000-000000000001.png"));
    await user.type(box, "what is wrong here?{Enter}");
    expect(onSend).toHaveBeenCalledWith(`what is wrong here?\n\nAttached: ${PATH1}`);
    await waitFor(() => expect(screen.queryByLabelText("Attachments to send")).toBeNull());
  });

  it("the clip button attaches a PDF, a chip can be removed, and a message may be only attachments", async () => {
    const user = userEvent.setup();
    const onSend = vi.fn();
    render(<Composer id="m" label="Message" placeholder="" onSend={onSend} />);
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    await user.upload(screen.getByLabelText("Choose files to attach"), [png("a.png"), pdf()]);
    expect(await screen.findByText("spec.pdf")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Remove a.png" }));
    await user.click(screen.getByRole("button", { name: "Send" }));
    expect(onSend).toHaveBeenCalledWith("Attached: /Users/you/.hadron/attachments/2026-11-09/00000000-0000-4000-8000-000000000002.pdf");
  });

  it("a dropped image is attached too, and a refused one is reported", async () => {
    const onError = vi.fn();
    const view = render(<Composer id="m" label="Message" placeholder="" onSend={vi.fn()} onError={onError} />);
    const big = png("big.png");
    Object.defineProperty(big, "size", { value: 21 * 1024 * 1024 });
    fireEvent.drop(view.container.querySelector(".composer")!, { dataTransfer: clip([png(), big]) });
    expect(await screen.findByText("shot.png")).toBeInTheDocument();
    expect(onError).toHaveBeenCalledWith("big.png is larger than 20 MB, the most Hadron attaches.");
  });

  it("the history shows a thumbnail for each attached image", () => {
    render(<ChatLine m={message({ author: "you", text: `see\n\nAttached: ${PATH1}` })} />);
    expect(screen.getByRole("img", { name: /Attached image/ })).toHaveAttribute("src", "/attachments/2026-11-09/00000000-0000-4000-8000-000000000001.png");
  });
});
