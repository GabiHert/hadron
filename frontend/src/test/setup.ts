import "@testing-library/jest-dom";

// jsdom has no ResizeObserver; the terminal uses one to refit.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// xterm probes for a canvas when it loads; jsdom has none and would log "not implemented".
HTMLCanvasElement.prototype.getContext = (() => null) as unknown as typeof HTMLCanvasElement.prototype.getContext;

// The theme module keeps its state between renders; each test starts from nothing.
// Imported late, so it is the copy the test file loaded with its mocked api.
import { afterEach } from "vitest";
afterEach(async () => {
  (await import("../theme")).resetThemesForTest();
  // A whenReady retry must not outlive the test: after teardown it would find no document.
  (await import("../commands")).cancelPendingReady();
  try {
    localStorage.clear();
  } catch {
    /* no storage */
  }
});
