export const slug = (t: string) =>
  t
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 40);

export const ROLES: [string, string][] = [
  ["needs-you", "Needs you"],
  ["active", "Active"],
  ["blocked", "Blocked"],
  ["done", "Done"],
  ["backlog", "Backlog"],
];

export const SWATCHES = ["#24408e", "#2f7a4b", "#b07a2a", "#b4322a", "#6b4fa0", "#4b5263"];

/** 14:03:09 as the user's clock shows it. */
export const clock = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
};

/** 14:03, for chat lines. */
export const hm = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
};

/** The first n characters of text on one line, with an ellipsis when cut. */
export const short = (text: string, n: number) => {
  const t = text.replace(/\s+/g, " ").trim();
  return t.length > n ? t.slice(0, n - 1) + "…" : t;
};

/** The first n bullets of release notes written as markdown lists, without the bullet. */
export function releaseHighlights(notes: string, n = 3): string[] {
  const out: string[] = [];
  for (const line of notes.split("\n")) {
    const m = /^\s*[-*]\s+(.*\S)\s*$/.exec(line);
    if (m) out.push(m[1].replace(/`/g, ""));
    if (out.length === n) break;
  }
  return out;
}

/** A folder with the macOS home folder (/Users/<name>) shortened to ~. */
export function tildePath(path: string): string {
  return path.replace(/^\/Users\/[^/]+(?=\/|$)/, "~");
}

/** The folder a card runs in when it is not the workspace root, else "". */
export function cardFolder(cwd: string | undefined, root: string | undefined): string {
  const clean = (p: string) => p.replace(/\/+$/, "");
  if (!cwd || clean(cwd) === clean(root ?? "")) return "";
  return cwd;
}

/** How near a list's top or bottom edge (px) a drag scrolls it, and the most it scrolls per dragover. */
export const EDGE_ZONE = 48;
const EDGE_STEP = 18;

/**
 * edgeScroll scrolls a list while a card is dragged near its top or bottom edge, faster the nearer
 * the pointer is, so a drop can reach cards out of view. dragover fires every few dozen milliseconds
 * while the pointer is held there, so each call moves the list a step. It returns the step taken.
 */
export function edgeScroll(el: HTMLElement | null, clientY: number): number {
  if (!el || el.scrollHeight <= el.clientHeight) return 0;
  const r = el.getBoundingClientRect();
  const zone = Math.min(EDGE_ZONE, r.height / 3);
  let step = 0;
  if (clientY < r.top + zone) step = -Math.ceil(EDGE_STEP * (1 - Math.max(0, clientY - r.top) / zone));
  else if (clientY > r.bottom - zone) step = Math.ceil(EDGE_STEP * (1 - Math.max(0, r.bottom - clientY) / zone));
  if (step) el.scrollTop += step;
  return step;
}
