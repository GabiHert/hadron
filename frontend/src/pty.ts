// The PTY bridge: the three methods Wails binds besides Invoke, and the pty:data:<id> event.
import { PtyAttach, PtyResize, PtyWrite } from "../wailsjs/go/app/App";
import { EventsOn } from "../wailsjs/runtime/runtime";

/** Output bytes [start, end) of a session, in base64. */
export interface PtyChunk {
  start: number;
  end: number;
  data: string;
}

export function bytesFromB64(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function b64FromText(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

/** What a terminal replays when it mounts, and the byte offset the replay stops at. */
export async function attach(sessionId: string): Promise<{ end: number; bytes: Uint8Array }> {
  const raw = await PtyAttach(sessionId);
  const cut = raw.indexOf(":");
  return { end: Number(raw.slice(0, cut)) || 0, bytes: bytesFromB64(raw.slice(cut + 1)) };
}

// Wails runs every bound call on its own goroutine, so two PtyWrite calls in flight at once can reach
// the PTY in either order: "/model" typed fast could arrive as "/mdoel", and Esc Esc or a dead key's
// composition could be split around another key. Each session's writes therefore go one at a time,
// in the order they were typed: a write starts only when the one before it has returned.
const tails = new Map<string, Promise<unknown>>();

/** Sends keystrokes to a session, after every earlier write to it has landed. */
export function write(sessionId: string, text: string): Promise<void> {
  const prev = tails.get(sessionId) ?? Promise.resolve();
  const next = prev.catch(() => undefined).then(() => PtyWrite(sessionId, b64FromText(text)));
  tails.set(sessionId, next);
  void next.finally(() => {
    if (tails.get(sessionId) === next) tails.delete(sessionId);
  }).catch(() => undefined);
  return next;
}
export const resize = (sessionId: string, cols: number, rows: number) => PtyResize(sessionId, cols, rows);

/** Subscribes to a session's output. Returns the unsubscribe function. */
export function onData(sessionId: string, cb: (chunk: PtyChunk) => void): () => void {
  return EventsOn(`pty:data:${sessionId}`, cb);
}
