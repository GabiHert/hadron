// Attachments: images and PDFs the user pastes, drops or picks for a terminal or a chat message.
// Their bytes go to attachment_save, which keeps a copy under ~/.hadron/attachments and returns its
// absolute path; Claude Code reads an image from a path. Files dropped on a terminal in the app keep
// their own paths, which Wails hands over through runtime.OnFileDrop.
import * as api from "./api";
import type { Attachment } from "./api";
import * as runtime from "../wailsjs/runtime/runtime";

/** The largest file Hadron attaches, as internal/attachments.MaxBytes. */
export const MAX_BYTES = 20 * 1024 * 1024;
/** The file picker's filter. */
export const ACCEPT = "image/png,image/jpeg,image/gif,image/webp,application/pdf";
const TYPES = new Set(ACCEPT.split(","));

/** True for a file Hadron attaches; an unknown type is left to the backend, which sniffs the bytes. */
export const attachable = (f: File) => TYPES.has(f.type) || f.type === "";

/** The files of a paste or drop that Hadron would attach (none when it holds only text). */
export function filesOf(dt: DataTransfer | null | undefined): File[] {
  if (!dt) return [];
  const out: File[] = [];
  const items = dt.items ? Array.from(dt.items) : [];
  if (items.length) {
    for (const it of items) {
      if (it.kind !== "file") continue;
      const f = it.getAsFile();
      if (f && TYPES.has(f.type)) out.push(f);
    }
    return out;
  }
  return Array.from(dt.files ?? []).filter((f) => TYPES.has(f.type));
}

function bufferOf(file: Blob): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === "function") return file.arrayBuffer();
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result as ArrayBuffer);
    r.onerror = () => reject(r.error ?? new Error("could not read the file"));
    r.readAsArrayBuffer(file);
  });
}

export async function base64Of(file: Blob): Promise<string> {
  const bytes = new Uint8Array(await bufferOf(file));
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

/** Saves each file, in order. A file that is too big or of another type is reported, not saved. */
export async function saveFiles(files: File[]): Promise<{ saved: Attachment[]; errors: string[] }> {
  const saved: Attachment[] = [];
  const errors: string[] = [];
  for (const f of files) {
    const name = f.name || "pasted image";
    if (f.size > MAX_BYTES) {
      errors.push(`${name} is larger than 20 MB, the most Hadron attaches.`);
      continue;
    }
    if (!attachable(f)) {
      errors.push(`${name} is not an image or a PDF.`);
      continue;
    }
    try {
      saved.push(await api.attachment_save({ name, data: await base64Of(f) }));
    } catch (e) {
      errors.push(e instanceof Error ? e.message : String(e));
    }
  }
  return { saved, errors };
}

/**
 * A path as a shell word, escaped the way macOS Terminal types a dropped file: every character
 * other than letters, digits and - _ . / ~ + , : @ % = gets a backslash.
 */
export function shellQuote(path: string): string {
  return path.replace(/[^A-Za-z0-9_\-./~+,:@%=]/g, (c) => "\\" + c);
}

/** What a terminal types for dropped or attached paths: each quoted, a space after each. */
export const typedPaths = (paths: string[]) => paths.map((p) => shellQuote(p) + " ").join("");

/** The line a chat message carries for each attachment, so the relayed text names the path. */
export const attachedLine = (path: string) => `Attached: ${path}`;

/** Appends the attachment lines to a message. */
export function withAttachments(text: string, paths: string[]): string {
  if (!paths.length) return text;
  const lines = paths.map(attachedLine).join("\n");
  return text ? `${text}\n\n${lines}` : lines;
}

// An absolute path to a file in an attachments folder: <home>/attachments/<date>/<uuid>.<ext>.
const ATTACHED = /(\/[^\s`'"<>]*\/attachments\/(\d{4}-\d{2}-\d{2})\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.(png|jpg|gif|webp|pdf))/g;

/** The attachments a message names, with the url of the thumbnail for an image. */
export function attachmentsIn(text: string): { path: string; url: string; pdf: boolean }[] {
  const out: { path: string; url: string; pdf: boolean }[] = [];
  const seen = new Set<string>();
  for (const m of text.matchAll(ATTACHED)) {
    if (seen.has(m[1])) continue;
    seen.add(m[1]);
    const pdf = m[4] === "pdf";
    out.push({ path: m[1], url: pdf ? "" : `/attachments/${m[2]}/${m[3]}.${m[4]}`, pdf });
  }
  return out;
}

/** True when the app runs in Wails with file drops on: a drop then arrives as paths through OnFileDrop. */
export const nativeFileDrop = () =>
  !!(window as unknown as { wails?: { flags?: { enableWailsDragAndDrop?: boolean } } }).wails?.flags?.enableWailsDragAndDrop;

// Wails takes one OnFileDrop callback for the whole window. It is registered once, and a drop goes
// to the zone under the pointer.
const zones = new Map<Element, (paths: string[]) => void>();
let listening = false;

function listen() {
  if (listening) return;
  try {
    runtime.OnFileDrop((x, y, paths) => {
      const at = document.elementFromPoint(x, y);
      for (const [el, cb] of zones) if (at && el.contains(at)) return cb(paths);
    }, false);
    listening = true;
  } catch {
    // no Wails runtime (tests, a plain browser): drops arrive as File objects only
  }
}

/** Calls cb with the absolute paths of files dropped on el. Returns the unregister function. */
export function onPathDrop(el: Element, cb: (paths: string[]) => void): () => void {
  zones.set(el, cb);
  listen();
  return () => zones.delete(el);
}

/** Test hook: deliver a native drop as Wails would. */
export function deliverPathDrop(el: Element, paths: string[]) {
  zones.get(el)?.(paths);
}
