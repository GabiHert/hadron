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

export const write = (sessionId: string, text: string) => PtyWrite(sessionId, b64FromText(text));
export const resize = (sessionId: string, cols: number, rows: number) => PtyResize(sessionId, cols, rows);

/** Subscribes to a session's output. Returns the unsubscribe function. */
export function onData(sessionId: string, cb: (chunk: PtyChunk) => void): () => void {
  return EventsOn(`pty:data:${sessionId}`, cb);
}
