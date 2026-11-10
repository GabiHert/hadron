import { vi } from "vitest";
import { cancelPendingReady, whenReady } from "./commands";

afterEach(() => vi.useRealTimers());

describe("whenReady", () => {
  it("retries until the element shows, and can be cancelled", () => {
    vi.useFakeTimers();
    let el: HTMLElement | null = null;
    const act = vi.fn();
    whenReady(() => el, act);
    vi.advanceTimersByTime(16 * 3);
    expect(act).not.toHaveBeenCalled();
    el = document.createElement("div");
    vi.advanceTimersByTime(16);
    expect(act).toHaveBeenCalledWith(el);

    const late = vi.fn();
    const cancel = whenReady(() => null, late);
    const find = vi.fn(() => null);
    whenReady(find, late);
    cancel();
    cancelPendingReady();
    const calls = find.mock.calls.length;
    vi.advanceTimersByTime(16 * 40);
    expect(find.mock.calls.length).toBe(calls);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("stops looking once there is no document (the test environment was torn down)", () => {
    vi.useFakeTimers();
    const find = vi.fn(() => null);
    whenReady(find, vi.fn());
    const doc = globalThis.document;
    // @ts-expect-error: simulating teardown
    delete globalThis.document;
    try {
      expect(() => vi.advanceTimersByTime(16 * 40)).not.toThrow();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      globalThis.document = doc;
    }
    expect(find).toHaveBeenCalledTimes(1);
  });
});
