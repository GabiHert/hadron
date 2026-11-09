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
