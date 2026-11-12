import { useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import { DEFAULTS, checkChord, chordOf, formatChord, fuzzyScore, keysOf, rebind, resetBinding, speakChord } from "../keys";
import type { CommandDef, Overrides, Scope } from "../keys";
import { useKeyBindings } from "../state/useKeys";

/** The scopes as Settings → Keyboard groups them, in that order. */
export const SCOPE_LABEL: [Scope, string, string][] = [
  ["global", "Everywhere", "Work in every zone. ⌘ chords also work in a text field."],
  ["board", "Board", "When the board has the focus and no text field does."],
  ["panel", "Planner and card panels", "When a chat panel has the focus. Single keys never fire while you type."],
  ["dashboard", "Dashboard", "When the dashboard has the focus."],
  ["workspaces", "Workspace bar", "When a workspace tab has the focus (⇧⌘W)."],
  ["terminal", "Terminal", "Leave a terminal without the key reaching Claude Code."],
];

/** The rows of the list, by scope, filtered by a search on the title, the id and the chords. */
export function keyboardRows(overrides: Overrides, q: string): { scope: Scope; label: string; hint: string; rows: CommandDef[] }[] {
  const query = q.trim();
  const match = (d: CommandDef) => {
    if (!query) return true;
    const chords = keysOf(d, overrides).map(formatChord).join(" ");
    return fuzzyScore(d.title, query) >= 0 || d.id.includes(query.toLowerCase()) || chords.toLowerCase().includes(query.toLowerCase());
  };
  return SCOPE_LABEL.map(([scope, label, hint]) => ({ scope, label, hint, rows: DEFAULTS.filter((d) => d.scope === scope && match(d)) })).filter((g) => g.rows.length);
}

type Pending = { id: string; chord: string; reason: string; canReplace: boolean };

/** Settings → Keyboard: every shortcut, searchable, with rebinding, conflict detection and reset. */
export function KeyboardTab({ toast }: { toast: (t: string) => void }) {
  const { overrides, save } = useKeyBindings(toast);
  const [q, setQ] = useState("");
  const [capturing, setCapturing] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const groups = keyboardRows(overrides, q);
  const changed = Object.keys(overrides).length;

  const capture = (id: string) => (e: ReactKeyboardEvent<HTMLButtonElement>) => {
    // While listening, every key is the new chord: nothing else in Hadron sees it.
    e.preventDefault();
    e.stopPropagation();
    if (e.key === "Escape" && !e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey) {
      setCapturing(null);
      return;
    }
    const chord = chordOf(e.nativeEvent);
    if (!chord) return; // a modifier on its own: wait for the key
    setCapturing(null);
    const check = checkChord(id, chord, overrides);
    if (check.ok) {
      void save(rebind(overrides, id, chord));
      setPending(null);
      return;
    }
    setPending({ id, chord, reason: check.reason, canReplace: check.conflicts.length > 0 });
  };

  return (
    <>
      <h4>Keyboard</h4>
      <p className="sub">
        Every shortcut in Hadron. Choose Change and press the new keys; Esc cancels. ? or ⌘/ shows this list anywhere, and ⌘K finds any command. The planner can change bindings too, through settings_update of keys.bindings.
      </p>
      <div className="inline">
        <input className="in" id="keys-q" type="search" aria-label="Search shortcuts" placeholder="Search shortcuts, such as dashboard or ⌘D" value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
        <button className="btn sm" type="button" disabled={!changed} onClick={() => { setPending(null); void save({}); }}>
          Reset all{changed ? ` (${changed})` : ""}
        </button>
      </div>
      {pending ? (
        <div className="box" role="alert" aria-label="Shortcut conflict">
          <span>{pending.reason}</span>
          <div className="inline" style={{ marginTop: 6 }}>
            {pending.canReplace ? (
              <button className="btn sm pri" type="button" onClick={() => { void save(rebind(overrides, pending.id, pending.chord, true)); setPending(null); }}>
                Use {formatChord(pending.chord)} here instead
              </button>
            ) : null}
            <button className="btn sm" type="button" onClick={() => setPending(null)}>Cancel</button>
          </div>
        </div>
      ) : null}
      <div className="scroll keys-list">
        {groups.map((g) => (
          <section key={g.scope} aria-label={`${g.label} shortcuts`}>
            <h5 className="cap" style={{ margin: "12px 0 2px" }}>{g.label}</h5>
            <p className="hint" style={{ margin: "0 0 6px" }}>{g.hint}</p>
            <table className="table">
              <tbody>
                {g.rows.map((d) => {
                  const keys = keysOf(d, overrides);
                  const custom = d.id in overrides;
                  const listening = capturing === d.id;
                  return (
                    <tr key={d.id} data-command={d.id}>
                      <td style={{ width: "50%" }}>{d.title}{d.confirm ? <span className="hint"> · asks first</span> : null}</td>
                      <td>
                        {keys.length ? (
                          keys.map((k) => <kbd key={k} className="kbd" aria-label={speakChord(k)} title={k}>{d.prefix ? `${formatChord(k)} …` : formatChord(k)}</kbd>)
                        ) : (
                          <span className="hint">none</span>
                        )}
                        {custom ? <span className="pill acc" style={{ marginLeft: 6 }}>changed</span> : null}
                      </td>
                      <td style={{ whiteSpace: "nowrap", textAlign: "right" }}>
                        <button
                          className={"btn sm" + (listening ? " pri" : "")}
                          type="button"
                          aria-label={listening ? `Press the new keys for ${d.title}` : `Change the shortcut for ${d.title}`}
                          onClick={() => { setPending(null); setCapturing(listening ? null : d.id); }}
                          onKeyDown={listening ? capture(d.id) : undefined}
                          onBlur={() => listening && setCapturing(null)}
                        >
                          {listening ? "Press keys…" : "Change"}
                        </button>{" "}
                        <button className="btn sm quiet" type="button" disabled={!custom} aria-label={`Reset ${d.title}`} onClick={() => void save(resetBinding(overrides, d.id))}>
                          Reset
                        </button>{" "}
                        <button className="btn sm quiet" type="button" disabled={!keys.length} aria-label={`Remove the shortcut for ${d.title}`} onClick={() => void save({ ...overrides, [d.id]: [] })}>
                          Remove
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </section>
        ))}
        {!groups.length ? <p className="hint">No shortcut matches “{q}”.</p> : null}
      </div>
    </>
  );
}
