import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { ChipColors, TagColor } from "../api";
import type { Board } from "../state/useBoard";
import { effectiveModel } from "../state/useBoard";
import { KEY_PATH, MODEL_PATH, STAGE_PATH, TAG_COLORS, keyPrefix, publishChipColors, tagClass, tagColor, useChipColors } from "../state/chipColors";
import { closeOnEscape } from "./ui";

/** One tag as the colour pickers see it: a field path, its value, and the label of the field. */
export interface Tag {
  path: string;
  /** the value as coloured: for card.key, the prefix */
  value: string;
  /** the text the pill shows */
  text: string;
  label: string;
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** The tags of one card: key, stage, linked and custom field chips, and the model. */
export function cardTags(board: Board, cardId: string): Tag[] {
  const c = board.cards.find((x) => x.id === cardId);
  if (!c) return [];
  const out: Tag[] = [];
  if (c.key) out.push({ path: KEY_PATH, value: keyPrefix(c.key), text: keyPrefix(c.key), label: "Key prefix" });
  if (c.stage) out.push({ path: STAGE_PATH, value: c.stage, text: c.stage, label: "Stage" });
  for (const ch of board.chips[c.id] ?? []) out.push({ path: ch.path, value: String(ch.value), text: String(ch.value), label: ch.label });
  const model = effectiveModel(c, board.profiles, board.ws);
  if (model) out.push({ path: MODEL_PATH, value: model.id, text: model.id, label: "Model" });
  return out;
}

/** A tag pill that opens its colour picker. */
export function TagButton({ wsId, tag, colors, toast }: { wsId: string; tag: Tag; colors: ChipColors; toast: (t: string) => void }) {
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  return (
    <span style={{ position: "relative" }}>
      <button ref={btn} className={tagClass(tagColor(colors, tag.path, tag.value)) + " tag"} aria-label={`Colour of ${tag.label} ${tag.text}`} aria-expanded={open} title={`${tag.label}: ${tag.text}. Choose its colour`} onClick={() => setOpen(!open)}>
        {tag.text}
      </button>
      {open ? <TagPicker wsId={wsId} tag={tag} colors={colors} toast={toast} onClose={() => { setOpen(false); btn.current?.focus(); }} /> : null}
    </span>
  );
}

/** The palette for one value, plus the field's automatic-colour switch. */
export function TagPicker({ wsId, tag, colors, toast, onClose }: { wsId: string; tag: Tag; colors: ChipColors; toast: (t: string) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => ref.current?.querySelector<HTMLElement>("button")?.focus(), []);
  const rule = colors.rules.find((r) => r.path === tag.path && r.value === tag.value)?.color ?? null;
  const auto = !colors.plain.includes(tag.path);
  const call = async (fn: () => Promise<ChipColors>) => {
    try {
      publishChipColors(wsId, await fn());
    } catch (e) {
      toast(errText(e));
    }
  };
  const pick = (color: TagColor | "") => void call(() => api.chip_color_set({ workspace_id: wsId, path: tag.path, value: tag.value, color })).then(onClose);
  return (
    <div className="tagpick" ref={ref} role="dialog" aria-label={`Colour for ${tag.label} ${tag.text}`} onKeyDown={closeOnEscape(onClose)}>
      <div className="hint">
        {tag.label}: <b>{tag.path === KEY_PATH ? `${tag.value}-…` : tag.value}</b>
      </div>
      <div className="swatches">
        {TAG_COLORS.map((c) => (
          <button key={c} className={`swatch tag-${c}`} aria-label={c} aria-pressed={rule === c} title={c} onClick={() => pick(c)}>
            A
          </button>
        ))}
      </div>
      <div className="inline">
        <button className="btn sm" disabled={!rule} onClick={() => pick("")}>
          {tag.path === KEY_PATH ? "No colour" : "Automatic"}
        </button>
        <div style={{ flex: 1 }} />
        <button className="btn sm quiet" onClick={onClose}>
          Close
        </button>
      </div>
      {tag.path !== KEY_PATH ? (
        <label className="inline hint">
          <input type="checkbox" checked={auto} onChange={(e) => void call(() => api.chip_color_auto_set({ workspace_id: wsId, path: tag.path, auto: e.target.checked }))} />
          Automatic colours for every {tag.label} value
        </label>
      ) : null}
    </div>
  );
}

/** The card panel's row of the card's tags, each opening its colour picker. */
export function CardTags({ board, cardId }: { board: Board; cardId: string }) {
  const wsId = board.ws?.id ?? "";
  const colors = useChipColors(wsId);
  const tags = cardTags(board, cardId);
  if (!tags.length) return null;
  return (
    <div className="tagrow" aria-label="Card tags" style={{ marginBottom: 6 }}>
      {tags.map((t) => (
        <TagButton key={t.path} wsId={wsId} tag={t} colors={colors} toast={board.actions.toast} />
      ))}
    </div>
  );
}

/** Settings → Tags: every tag value on the board (and every coloured one), by field, with the field's automatic switch. */
export function TagsTab({ board }: { board: Board }) {
  const wsId = board.ws?.id ?? "";
  const colors = useChipColors(wsId);
  const groups = new Map<string, { label: string; tags: Map<string, Tag> }>();
  const add = (t: Tag) => {
    const g = groups.get(t.path) ?? { label: t.label, tags: new Map<string, Tag>() };
    if (!g.tags.has(t.value)) g.tags.set(t.value, t.path === KEY_PATH ? { ...t, text: t.value } : t);
    groups.set(t.path, g);
  };
  add({ path: STAGE_PATH, value: "", text: "", label: "Stage" });
  for (const c of board.cards) cardTags(board, c.id).forEach(add);
  for (const r of colors.rules) add({ path: r.path, value: r.value, text: r.value, label: groups.get(r.path)?.label ?? r.path });
  const call = async (fn: () => Promise<ChipColors>) => {
    try {
      publishChipColors(wsId, await fn());
    } catch (e) {
      board.actions.toast(errText(e));
    }
  };
  return (
    <>
      <h4>Tags</h4>
      <p className="sub">
        Tags are the pills on a card: its key, stage, linked and custom fields, and model. A value you colour keeps its colour; the others get a steady automatic one unless you switch that off for the field. Key prefixes stay plain until you colour them. The planner can set these too.
      </p>
      {[...groups.entries()].map(([path, g]) => {
        const tags = [...g.tags.values()].filter((t) => t.value);
        return (
          <div className="box" key={path} role="group" aria-label={`Tags of ${g.label}`}>
            <div className="boxh">
              <b>{g.label}</b>
              <span className="hint mono">{path}</span>
              <div style={{ flex: 1 }} />
              {path !== KEY_PATH ? (
                <button className={"switch" + (colors.plain.includes(path) ? "" : " on")} role="switch" aria-checked={!colors.plain.includes(path)} aria-label={`Automatic colours for ${g.label}`} title="Automatic colours"
                  onClick={() => void call(() => api.chip_color_auto_set({ workspace_id: wsId, path, auto: colors.plain.includes(path) }))} />
              ) : null}
            </div>
            <div className="rrow tagrow">
              {tags.length ? tags.map((t) => <TagButton key={t.value} wsId={wsId} tag={t} colors={colors} toast={board.actions.toast} />) : <span className="hint">No values on the board yet.</span>}
            </div>
          </div>
        );
      })}
    </>
  );
}
