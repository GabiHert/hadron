import "@testing-library/jest-dom";

// jsdom has no ResizeObserver; the terminal uses one to refit.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// xterm probes for a canvas when it loads; jsdom has none and would log "not implemented".
HTMLCanvasElement.prototype.getContext = (() => null) as unknown as typeof HTMLCanvasElement.prototype.getContext;
