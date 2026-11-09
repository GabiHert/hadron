// Stand-ins for xterm.js and the PTY bridge, so terminal behaviour can be tested in jsdom (which has
// no canvas). FakeXTerm records what the component does to it; ptyMock lets a test decide when the
// replay arrives and push live output.
import { vi } from "vitest";
import type { PtyChunk } from "../pty";

type KeyHandler = (e: Partial<KeyboardEvent> & { type: string; key: string }) => boolean;

export class FakeXTerm {
  static instances: FakeXTerm[] = [];
  cols = 100;
  rows = 30;
  options: Record<string, unknown> = {};
  written: (string | Uint8Array)[] = [];
  disposed = false;
  focused = false;
  keyHandler: KeyHandler = () => true;
  dataHandlers: ((d: string) => void)[] = [];
  resizeHandlers: ((s: { cols: number; rows: number }) => void)[] = [];
  constructor(opts: Record<string, unknown> = {}) {
    this.options = opts;
    FakeXTerm.instances.push(this);
  }
  loadAddon() {}
  open() {}
  focus() { this.focused = true; }
  write(d: string | Uint8Array) { this.written.push(d); }
  attachCustomKeyEventHandler(h: KeyHandler) { this.keyHandler = h; }
  onData(cb: (d: string) => void) {
    this.dataHandlers.push(cb);
    return { dispose: () => { this.dataHandlers = this.dataHandlers.filter((x) => x !== cb); } };
  }
  onResize(cb: (s: { cols: number; rows: number }) => void) {
    this.resizeHandlers.push(cb);
    return { dispose: () => { this.resizeHandlers = this.resizeHandlers.filter((x) => x !== cb); } };
  }
  pasted: string[] = [];
  /** Like xterm's paste: the text goes out through onData (no bracketed-paste mode in the fake). */
  paste(d: string) { this.pasted.push(d); this.dataHandlers.forEach((h) => h(d)); }
  dispose() { this.disposed = true; }
  /** What the user typed or the app wrote, as text. */
  text() { return this.written.map((w) => (typeof w === "string" ? w : new TextDecoder().decode(w))).join(""); }
}

export class FakeFit {
  fit = vi.fn();
}

export const xtermModule = { Terminal: FakeXTerm };
export const fitModule = { FitAddon: FakeFit };

const handlers = new Map<string, (c: PtyChunk) => void>();
const pending: ((v: { end: number; bytes: Uint8Array }) => void)[] = [];
const text = (s: string) => new TextEncoder().encode(s);

export const ptyMock = {
  /** Holds each replay until the test calls resolveAttach. */
  attach: vi.fn(
    () =>
      new Promise<{ end: number; bytes: Uint8Array }>((resolve) => {
        pending.push(resolve);
      }),
  ),
  write: vi.fn(async () => {}),
  resize: vi.fn(async () => {}),
  onData: vi.fn((id: string, cb: (c: PtyChunk) => void) => {
    handlers.set(id, cb);
    return () => handlers.delete(id);
  }),
  bytesFromB64: (b64: string) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0)),
  b64FromText: (t: string) => btoa(t),
};

/** Resolves an attach call: the newest by default, or the nth (0 is the first). */
export const resolveAttach = (replay: string, end = replay.length, nth = pending.length - 1) => pending[nth]?.({ end, bytes: text(replay) });
export const emitPty = (id: string, start: number, data: string) =>
  handlers.get(id)?.({ start, end: start + data.length, data: btoa(data) });
export const subscribed = (id: string) => handlers.has(id);
export const resetTerm = () => {
  FakeXTerm.instances = [];
  handlers.clear();
  pending.length = 0;
  for (const f of [ptyMock.attach, ptyMock.write, ptyMock.resize, ptyMock.onData]) f.mockClear();
};
