import { ambientOff, ambientOn, ambientPlaying, AMBIENT_LEVEL, uiSound, UI_LEVEL } from "./tourAudio";

// A small fake of the Web Audio graph: it records the oscillators made and every gain set.
class FakeParam {
  value = 0;
  set: number[] = [];
  setValueAtTime(v: number) { this.value = v; this.set.push(v); }
  linearRampToValueAtTime(v: number) { this.value = v; this.set.push(v); }
  exponentialRampToValueAtTime(v: number) { this.value = v; this.set.push(v); }
  setTargetAtTime(v: number) { this.value = v; this.set.push(v); }
  cancelScheduledValues() {}
}
class FakeNode {
  connect(n: unknown) { return n; }
}
class FakeOsc extends FakeNode {
  type = "sine";
  frequency = new FakeParam();
  detune = new FakeParam();
  started = false;
  stopped = false;
  start() { this.started = true; }
  stop() { this.stopped = true; }
}
class FakeGain extends FakeNode { gain = new FakeParam(); }
class FakeFilter extends FakeNode { type = ""; frequency = new FakeParam(); Q = new FakeParam(); }
const made: { osc: FakeOsc[]; gains: FakeGain[] } = { osc: [], gains: [] };
class FakeCtx {
  state = "running";
  currentTime = 0;
  destination = new FakeNode();
  createOscillator() { const o = new FakeOsc(); made.osc.push(o); return o; }
  createGain() { const g = new FakeGain(); made.gains.push(g); return g; }
  createBiquadFilter() { return new FakeFilter(); }
  resume() { return Promise.resolve(); }
}

beforeAll(() => {
  (globalThis as unknown as { AudioContext: unknown }).AudioContext = FakeCtx;
});
afterAll(() => {
  delete (globalThis as unknown as { AudioContext?: unknown }).AudioContext;
});
beforeEach(() => {
  made.osc = [];
  made.gains = [];
});

describe("the tutorial's window sounds", () => {
  it("clicks at a whisper of the Sounds volume, and not at all at 0", () => {
    uiSound("hover", 0);
    expect(made.osc).toHaveLength(0);
    uiSound("press", 0.5);
    expect(made.osc).toHaveLength(1);
    expect(Math.max(...made.gains[0].gain.set)).toBeCloseTo(UI_LEVEL.press * 0.5);
    expect(UI_LEVEL.hover).toBeLessThan(UI_LEVEL.press);
  });

  it("fades the ambient pad in under everything else, and out again", () => {
    ambientOn(0.6);
    expect(ambientPlaying()).toBe(true);
    const out = made.gains[0];
    expect(out.gain.set[0]).toBe(0);
    expect(out.gain.value).toBeCloseTo(0.6 * AMBIENT_LEVEL);
    expect(AMBIENT_LEVEL).toBeLessThan(UI_LEVEL.press);
    const voices = made.osc.length;
    ambientOn(0.3);
    expect(made.osc.length).toBe(voices);
    expect(out.gain.value).toBeCloseTo(0.3 * AMBIENT_LEVEL);
    ambientOff();
    expect(ambientPlaying()).toBe(false);
    expect(out.gain.value).toBe(0);
    expect(made.osc.every((o) => o.stopped)).toBe(true);
  });
});
