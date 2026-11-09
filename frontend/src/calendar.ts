// Pure helpers for the heatmap calendar (GitHub-contributions style). No React, no locale APIs:
// dates are handled as day numbers (days since 1970-01-01, computed from year/month/day) so
// results do not depend on the time zone of the machine running them.

export interface CalGroup { key: string; label: string; count: number }
export interface CalCell { date: string; count: number; level: 0 | 1 | 2 | 3 | 4; future: boolean; hasData: boolean }
export interface CalMonth { col: number; label: string }
export interface Calendar {
  weeks: number;
  /** columns[col][row], row 0 = Monday ... 6 = Sunday */
  columns: CalCell[][];
  months: CalMonth[];
  weekdayLabels: string[];
  total: number;
  totalText: string;
}

export const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"] as const;
export const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"] as const;

const DAY_MS = 86400000;
const toDayNum = (y: number, m: number, d: number) => Math.round(Date.UTC(y, m - 1, d) / DAY_MS);
const fromDayNum = (n: number) => {
  const dt = new Date(n * DAY_MS);
  return { y: dt.getUTCFullYear(), m: dt.getUTCMonth() + 1, d: dt.getUTCDate() };
};
const pad = (n: number, w = 2) => String(n).padStart(w, "0");
const iso = (n: number) => {
  const { y, m, d } = fromDayNum(n);
  return `${pad(y, 4)}-${pad(m)}-${pad(d)}`;
};
/** 0 = Monday ... 6 = Sunday */
const weekdayOf = (n: number) => (new Date(n * DAY_MS).getUTCDay() + 6) % 7;

const ISO_RE = /^(\d{4})-(\d{2})-(\d{2})$/;
function parseISO(key: string): number | null {
  const m = ISO_RE.exec(key);
  if (!m) return null;
  const y = +m[1], mo = +m[2], d = +m[3];
  const n = toDayNum(y, mo, d);
  const back = fromDayNum(n);
  return back.y === y && back.m === mo && back.d === d ? n : null;
}

/** True when there is at least one group and every key is a real YYYY-MM-DD date. */
export function allISODates(groups: { key: string }[]): boolean {
  return groups.length > 0 && groups.every((g) => parseISO(g.key) !== null);
}

export const clampWeeks = (w?: number) => {
  const n = typeof w === "number" && Number.isFinite(w) ? Math.floor(w) : 26;
  return Math.min(53, Math.max(1, n));
};

/** "deploys" -> "deploy" when n is 1. */
export function unitFor(n: number, unit: string): string {
  return n === 1 && unit.endsWith("s") ? unit.slice(0, -1) : unit;
}

export function levelFor(count: number, max: number): 0 | 1 | 2 | 3 | 4 {
  if (count <= 0) return 0;
  return Math.min(4, Math.ceil((4 * count) / Math.max(1, max))) as 1 | 2 | 3 | 4;
}

export function buildCalendar(groups: CalGroup[], opts: { weeks?: number; now: Date; unit?: string }): Calendar {
  const weeks = clampWeeks(opts.weeks);
  const today = toDayNum(opts.now.getFullYear(), opts.now.getMonth() + 1, opts.now.getDate());
  const lastMonday = today - weekdayOf(today);
  const start = lastMonday - (weeks - 1) * 7;
  const end = start + weeks * 7 - 1;

  const counts = new Map<number, number>();
  for (const g of groups) {
    const n = parseISO(g.key);
    if (n === null || n < start || n > end) continue;
    counts.set(n, (counts.get(n) ?? 0) + g.count);
  }
  let max = 1;
  let total = 0;
  for (const [n, c] of counts) {
    if (n <= today) {
      max = Math.max(max, c);
      total += c;
    }
  }

  const columns: CalCell[][] = [];
  for (let c = 0; c < weeks; c++) {
    const col: CalCell[] = [];
    for (let r = 0; r < 7; r++) {
      const n = start + c * 7 + r;
      const future = n > today;
      const count = future ? 0 : counts.get(n) ?? 0;
      col.push({ date: iso(n), count, level: levelFor(count, max), future, hasData: count > 0 });
    }
    columns.push(col);
  }

  const months: CalMonth[] = [];
  const push = (col: number, label: string) => {
    const prev = months[months.length - 1];
    if (prev && col === prev.col + 1) return;
    months.push({ col, label });
  };
  for (let c = 0; c < weeks; c++) {
    const first = start + c * 7;
    let label: string | null = null;
    for (let r = 0; r < 7; r++) {
      const { m, d } = fromDayNum(first + r);
      if (d === 1) label = MONTHS[m - 1];
    }
    if (label === null && c === 0) label = MONTHS[fromDayNum(first).m - 1];
    if (label !== null) push(c, label);
  }

  const weekdayLabels = WEEKDAYS.map((w, i) => (i % 2 === 0 && i <= 4 ? w : ""));
  const unit = opts.unit || "events";
  return {
    weeks,
    columns,
    months,
    weekdayLabels,
    total,
    totalText: `${total} ${unitFor(total, unit)} in the last ${weeks} ${weeks === 1 ? "week" : "weeks"}`,
  };
}

/** "Mon 3 Nov · 4" or, with a unit, "Mon 3 Nov · 4 deploys". */
export function tooltip(cell: { date: string; count: number }, unit?: string): string {
  const n = parseISO(cell.date) ?? 0;
  const { m, d } = fromDayNum(n);
  const head = `${WEEKDAYS[weekdayOf(n)]} ${d} ${MONTHS[m - 1]}`;
  return unit ? `${head} · ${cell.count} ${unitFor(cell.count, unit)}` : `${head} · ${cell.count}`;
}
