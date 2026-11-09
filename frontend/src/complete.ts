// The chat composer's "/" and "@" menus: when they open, how they match, and what a pick inserts.
// Pure functions; components/Suggest.tsx draws the menu.

/** A fuzzy match: its score (higher is better) and the indexes of the matched characters. */
export interface Match {
  score: number;
  idx: number[];
}

const isSep = (c: string | undefined) => c === undefined || c === "/" || c === "-" || c === "_" || c === "." || c === " " || c === ":";

/**
 * Matches q against s, ignoring case: every character of q must appear in s, in order. A plain
 * substring beats a scattered match, a match in the last path segment or at the start of a word
 * scores higher, and a shorter s wins a tie. The same rules as internal/complete.Score.
 */
export function fuzzy(q: string, s: string): Match | null {
  if (!q) return { score: 0, idx: [] };
  const ql = q.toLowerCase();
  const sl = s.toLowerCase();
  const base = s.lastIndexOf("/") + 1;
  let at = sl.indexOf(ql, base);
  if (at < 0) at = sl.indexOf(ql);
  if (at >= 0) {
    let score = 1000 - s.length;
    if (at >= base) score += 400 + (at === base ? 300 : 0);
    else if (at === 0 || isSep(sl[at - 1])) score += 150;
    return { score, idx: Array.from({ length: ql.length }, (_, i) => at + i) };
  }
  const idx: number[] = [];
  let score = 0;
  let prev = -2;
  for (let si = 0, qi = 0; si < sl.length && qi < ql.length; si++) {
    if (sl[si] !== ql[qi]) continue;
    let pts = 1;
    if (si === prev + 1) pts += 6;
    if (si === 0 || isSep(sl[si - 1])) pts += 8;
    if (si >= base) pts += 3;
    score += pts;
    idx.push(si);
    prev = si;
    qi++;
  }
  if (idx.length < ql.length) return null;
  return { score: score * 10 - s.length, idx };
}

/** What the caret is in: a command at the start of the message, or an @ mention. */
export interface Trigger {
  kind: "/" | "@";
  /** where the token starts (the / or the @) */
  start: number;
  /** where the token ends: the next whitespace or the end of the text */
  end: number;
  /** what is typed between the sign and the caret */
  query: string;
}

const space = /\s/;

/**
 * The menu the caret calls for. "/" opens only when the message starts with one slash and the caret
 * is still in that first word ("//" is the escape for a literal slash). "@" opens in a word that
 * starts with @ at the start of the message or after whitespace, so an email address does not.
 */
export function findTrigger(v: string, caret: number): Trigger | null {
  if (caret < 0 || caret > v.length) return null;
  if (v.startsWith("/") && !v.startsWith("//") && caret >= 1 && !space.test(v.slice(0, caret))) {
    const ws = v.search(space);
    return { kind: "/", start: 0, end: ws < 0 ? v.length : ws, query: v.slice(1, caret) };
  }
  let i = caret;
  while (i > 0 && !space.test(v[i - 1])) i--;
  if (v[i] === "@" && caret > i) {
    let end = caret;
    while (end < v.length && !space.test(v[end])) end++;
    return { kind: "@", start: i, end, query: v.slice(i + 1, caret) };
  }
  return null;
}

/** Replaces the trigger's token with text. A space follows unless it ends in "/" (a folder, to keep typing). */
export function applyPick(v: string, t: Trigger, text: string): { value: string; caret: number } {
  const after = v.slice(t.end);
  const folder = text.endsWith("/");
  const spaced = after.startsWith(" ");
  const pad = folder || spaced || after.startsWith("\n") ? "" : " ";
  const value = v.slice(0, t.start) + text + pad + after;
  // The caret lands after the space, whether it was added or already there.
  const caret = t.start + text.length + (folder ? 0 : pad ? 1 : spaced ? 1 : 0);
  return { value, caret };
}

/** The mention Claude Code reads for a path: @path, quoted when it has a space; a folder ends in /. */
export function pathMention(path: string, isDir: boolean): string {
  const p = isDir && !path.endsWith("/") ? path + "/" : path;
  return /\s/.test(p) ? `@"${p}"` : `@${p}`;
}

/** True for a message that goes to the session's terminal: one leading slash and a name. */
export const isCommand = (text: string) => /^\/[^/\s]/.test(text);

/** A message that starts with "//" is a literal slash for the planner: one slash is dropped. */
export const unescapeSlash = (text: string) => (text.startsWith("//") ? text.slice(1) : text);
