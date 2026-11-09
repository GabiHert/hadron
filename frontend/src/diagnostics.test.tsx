import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { DiagnosticsTab } from "./components/DiagnosticsTab";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { installErrorLogging } from "./errorlog";
import { mocks, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
const openURL = vi.fn();
vi.mock("../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: (...a: unknown[]) => openURL(...a), EventsOn: () => () => {} }));

beforeEach(() => {
  reset();
  openURL.mockReset();
  mocks.logs_tail.mockResolvedValue([]);
});

const line = (over = {}) => ({ time: "2026-11-08T12:34:56Z", level: "info", msg: "session started", fields: { card: "c1" }, raw: "{}", ...over });

describe("Diagnostics tab", () => {
  it("toggling the switch calls debug_logging_set", async () => {
    render(<DiagnosticsTab toast={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "Debug logging" }));
    await waitFor(() => expect(mocks.debug_logging_set).toHaveBeenCalledWith({ enabled: true }));
  });

  it("renders the tail and passes the level filter", async () => {
    mocks.logs_tail.mockResolvedValue([line(), line({ level: "error", msg: "boom" })]);
    render(<DiagnosticsTab toast={vi.fn()} />);
    expect(await screen.findByText(/session started card=c1/)).toBeInTheDocument();
    expect(screen.getByText(/12:34:56/, { selector: "div.bad" })).toHaveTextContent("boom");
    await userEvent.selectOptions(screen.getByLabelText("Level"), "error");
    await waitFor(() => expect(mocks.logs_tail).toHaveBeenLastCalledWith(expect.objectContaining({ level: "error", lines: 200 })));
  });

  it("shows the empty state", async () => {
    render(<DiagnosticsTab toast={vi.fn()} />);
    expect(await screen.findByText(/No log lines yet/)).toBeInTheDocument();
  });

  it("Copy diagnostics writes the version and a log line to the clipboard", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    mocks.diagnostics_get.mockResolvedValue({ version: "v9.9.9", macos: "15.0", arch: "arm64", home: "/tmp", log_dir: "/tmp/l", debug_logging: true, actions: 1, settings: {}, recent_log_lines: ["a log line"] });
    const toast = vi.fn();
    render(<DiagnosticsTab toast={toast} />);
    await userEvent.click(screen.getByRole("button", { name: "Copy diagnostics" }));
    await waitFor(() => expect(toast).toHaveBeenCalledWith("Diagnostics copied."));
    const text = (writeText.mock.calls[0] as unknown as string[])[0];
    expect(text).toContain("v9.9.9");
    expect(text).toContain("a log line");
  });

  it("Show in Finder opens a file:// URL", async () => {
    mocks.diagnostics_get.mockResolvedValue({ version: "v", macos: "", arch: "", home: "", log_dir: "/tmp/my logs", debug_logging: false, actions: 0, settings: {}, recent_log_lines: [] });
    render(<DiagnosticsTab toast={vi.fn()} />);
    await screen.findByText("/tmp/my logs");
    await userEvent.click(screen.getByRole("button", { name: "Show in Finder" }));
    expect(openURL).toHaveBeenCalledWith("file:///tmp/my%20logs");
  });
});

describe("error capture", () => {
  it("forwards an error and a rejection once each and de-duplicates a repeat", () => {
    const off = installErrorLogging(window);
    window.dispatchEvent(new ErrorEvent("error", { message: "kaput", error: new Error("kaput") }));
    window.dispatchEvent(new ErrorEvent("error", { message: "kaput", error: new Error("kaput") }));
    const ev = new Event("unhandledrejection") as Event & { reason: unknown };
    ev.reason = new Error("rejected");
    window.dispatchEvent(ev);
    off();
    expect(mocks.client_log).toHaveBeenCalledTimes(2);
    expect(mocks.client_log).toHaveBeenNthCalledWith(1, expect.objectContaining({ level: "error", message: "kaput", stack: expect.any(String) }));
    expect(mocks.client_log).toHaveBeenNthCalledWith(2, expect.objectContaining({ message: "rejected" }));
  });

  it("the error boundary logs a render crash", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const Bomb = () => { throw new Error("render exploded"); };
    render(<ErrorBoundary><Bomb /></ErrorBoundary>);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(mocks.client_log).toHaveBeenCalledWith(expect.objectContaining({ level: "error", message: "render exploded" }));
  });
});
