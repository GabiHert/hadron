// The tutorial's live sounds, made in the window with Web Audio: the tiny hover and press clicks on
// its buttons (too frequent and too latency-sensitive for a process per sound) and the optional
// ambient pad, which has to fade in and out with the tour. The step and illustration sounds are
// files played by Hadron's player (sound_play). Everything here follows the same Sounds setting and
// volume, which the caller passes in; without Web Audio (tests) it all does nothing.

type Ctx = AudioContext;

let ctx: Ctx | null = null;

function audio(): Ctx | null {
  if (ctx) return ctx;
  const AC = (globalThis as unknown as { AudioContext?: typeof AudioContext; webkitAudioContext?: typeof AudioContext }).AudioContext
    ?? (globalThis as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!AC) return null;
  try {
    ctx = new AC();
  } catch {
    return null;
  }
  return ctx;
}

/** A context that may have started suspended (no gesture yet) resumes on the first key or click. */
function wake(c: Ctx) {
  if (c.state === "suspended") void c.resume().catch(() => undefined);
}

/** Loudness relative to the Sounds volume: the hover tick is a whisper, the press a soft click. */
export const UI_LEVEL = { hover: 0.035, press: 0.07 };
/** The ambient pad's level relative to the Sounds volume: very quiet, under everything else. */
export const AMBIENT_LEVEL = 0.045;

/** A tiny click: a hover over a button or chip, or a press. */
export function uiSound(kind: "hover" | "press", volume: number) {
  const c = audio();
  if (!c || volume <= 0) return;
  wake(c);
  const t = c.currentTime;
  const o = c.createOscillator();
  const g = c.createGain();
  o.type = "sine";
  o.frequency.setValueAtTime(kind === "hover" ? 2400 : 1250, t);
  if (kind === "press") o.frequency.exponentialRampToValueAtTime(900, t + 0.04);
  const peak = UI_LEVEL[kind] * volume;
  const len = kind === "hover" ? 0.03 : 0.05;
  g.gain.setValueAtTime(0, t);
  g.gain.linearRampToValueAtTime(peak, t + 0.003);
  g.gain.exponentialRampToValueAtTime(0.0001, t + len);
  o.connect(g).connect(c.destination);
  o.start(t);
  o.stop(t + len + 0.02);
}

interface Pad {
  out: GainNode;
  nodes: AudioScheduledSourceNode[];
}
let pad: Pad | null = null;

/**
 * Starts the ambient pad, or sets its level when it plays: a soft, slowly breathing chord (C, G, E
 * and a high D, gently detuned) through a low-pass, fading in over two seconds.
 */
export function ambientOn(volume: number) {
  const c = audio();
  if (!c) return;
  wake(c);
  const level = Math.max(0, volume) * AMBIENT_LEVEL;
  const t = c.currentTime;
  if (pad) {
    pad.out.gain.cancelScheduledValues(t);
    pad.out.gain.setTargetAtTime(level, t, 0.6);
    return;
  }
  const out = c.createGain();
  out.gain.setValueAtTime(0, t);
  out.gain.linearRampToValueAtTime(level, t + 2);
  const lp = c.createBiquadFilter();
  lp.type = "lowpass";
  lp.frequency.value = 900;
  lp.Q.value = 0.4;
  lp.connect(out).connect(c.destination);
  const nodes: AudioScheduledSourceNode[] = [];
  const freqs = [130.81, 196.0, 329.63, 587.33];
  freqs.forEach((f, i) => {
    for (const detune of [-6, 6]) {
      const o = c.createOscillator();
      o.type = i === 0 ? "sine" : "triangle";
      o.frequency.value = f;
      o.detune.value = detune;
      const g = c.createGain();
      g.gain.value = (i === 3 ? 0.12 : 0.28) / freqs.length;
      // Each voice breathes at its own slow rate.
      const lfo = c.createOscillator();
      const depth = c.createGain();
      lfo.frequency.value = 0.07 + i * 0.03;
      depth.gain.value = g.gain.value * 0.6;
      lfo.connect(depth).connect(g.gain);
      o.connect(g).connect(lp);
      o.start(t);
      lfo.start(t);
      nodes.push(o, lfo);
    }
  });
  pad = { out, nodes };
}

/** Fades the ambient pad out over a second and a half, then stops it. */
export function ambientOff() {
  const c = ctx;
  const p = pad;
  if (!c || !p) return;
  pad = null;
  const t = c.currentTime;
  p.out.gain.cancelScheduledValues(t);
  p.out.gain.setValueAtTime(p.out.gain.value, t);
  p.out.gain.linearRampToValueAtTime(0, t + 1.5);
  for (const n of p.nodes) n.stop(t + 1.6);
}

/** Whether the pad is playing; for tests. */
export const ambientPlaying = () => pad !== null;
