import { useEffect, useState } from "react";
import * as api from "../api";
import type { ChipColors, TagColor } from "../api";

/** Every tag colour, in palette order. The theme defines --tag-<name> and --tag-<name>-b for each. */
export const TAG_COLORS: TagColor[] = ["grey", "blue", "teal", "green", "lime", "amber", "orange", "red", "pink", "purple"];
/** Automatic colours skip grey, which means "plain". */
const AUTO: TagColor[] = TAG_COLORS.filter((c) => c !== "grey");

export const KEY_PATH = "card.key";
export const STAGE_PATH = "card.stage";
export const MODEL_PATH = "card.model";

/** The part of a card key that card.key colours: ACME for ACME-508. */
export function keyPrefix(key: string): string {
  const k = key.trim();
  const i = k.lastIndexOf("-");
  return (i > 0 ? k.slice(0, i) : k).toUpperCase();
}

/** A stable palette colour for a value (FNV-1a over its text), the same on every launch. */
export function autoColor(value: string): TagColor {
  let h = 0x811c9dc5;
  for (let i = 0; i < value.length; i++) {
    h ^= value.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return AUTO[h % AUTO.length];
}

export const EMPTY: ChipColors = { palette: TAG_COLORS, rules: [], plain: [] };

/**
 * A tag's colour: the one set for the value, else an automatic one, or null for plain grey (when
 * the field's automatic colours are off, and for key prefixes, which have none).
 */
export function tagColor(cc: ChipColors, path: string, value: unknown): TagColor | null {
  const v = path === KEY_PATH ? keyPrefix(String(value ?? "")) : String(value ?? "").trim();
  if (!v) return null;
  const rule = cc.rules.find((r) => r.path === path && r.value === v);
  if (rule) return rule.color;
  if (path === KEY_PATH || cc.plain.includes(path)) return null;
  return autoColor(v);
}

/** The class a tag pill gets: "pill tag-blue", or "pill" for plain. */
export const tagClass = (c: TagColor | null) => (c ? `pill tag-${c}` : "pill");

// One copy per workspace, shared by the board, the card panel and Settings.
const cache = new Map<string, ChipColors>();
const subs = new Set<() => void>();
const inflight = new Set<string>();

async function load(wsId: string) {
  if (inflight.has(wsId)) return;
  inflight.add(wsId);
  try {
    cache.set(wsId, await api.chip_color_list({ workspace_id: wsId }));
    subs.forEach((f) => f());
  } catch {
    /* keep the last colours; the next change event tries again */
  } finally {
    inflight.delete(wsId);
  }
}

/** Stores the answer of a chip_color_* call, so every view updates at once. */
export function publishChipColors(wsId: string, cc: ChipColors) {
  cache.set(wsId, cc);
  subs.forEach((f) => f());
}

export function resetChipColorsForTests() {
  cache.clear();
}

/** A workspace's tag colours, reloaded when they change. */
export function useChipColors(wsId: string): ChipColors {
  const [, bump] = useState(0);
  useEffect(() => {
    const f = () => bump((n) => n + 1);
    subs.add(f);
    if (wsId && !cache.has(wsId)) void load(wsId);
    const off = api.onChanged((e) => {
      if (e.workspace_id === wsId && e.entities.includes("chip_colors")) void load(wsId);
    });
    return () => {
      subs.delete(f);
      off();
    };
  }, [wsId]);
  return cache.get(wsId) ?? EMPTY;
}
