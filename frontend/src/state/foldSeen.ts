/**
 * The cards a collapsed column already held, so its rail can badge the ones that arrived since.
 * Kept per column in localStorage: a convenience that survives a restart, and that starts over
 * (no badge) when the storage is empty or blocked.
 */
const KEY = "hadron.fold.seen";

type Seen = Record<string, string[]>;

function load(): Seen {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "{}");
    return v && typeof v === "object" ? (v as Seen) : {};
  } catch {
    return {};
  }
}

function save(s: Seen) {
  try {
    localStorage.setItem(KEY, JSON.stringify(s));
  } catch {
    /* storage is off: the badge just starts over */
  }
}

/** Records the cards a column holds as seen (when it collapses). */
export function markSeen(columnId: string, cardIds: string[]) {
  const s = load();
  s[columnId] = [...cardIds];
  save(s);
}

/** Forgets a column's seen cards (when it expands). */
export function clearSeen(columnId: string) {
  const s = load();
  if (!(columnId in s)) return;
  delete s[columnId];
  save(s);
}

/**
 * The cards of a collapsed column that arrived after it collapsed. A column with no record (collapsed
 * before this was kept, or over MCP) takes its current cards as seen, so nothing old is badged.
 */
export function unseen(columnId: string, cardIds: string[]): string[] {
  const s = load();
  const seen = s[columnId];
  if (!seen) {
    markSeen(columnId, cardIds);
    return [];
  }
  const set = new Set(seen);
  return cardIds.filter((id) => !set.has(id));
}
