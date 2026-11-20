// The frontend tests' load-tolerant deadlines, the counterpart of Go's internal/testwait. A test
// that waits polls (findBy*, waitFor), so a fast machine finishes as soon as the page settles; the
// deadlines only decide when a busy Mac (a release build at load 30+) gives up.
// HADRON_TEST_WAIT_SCALE multiplies both, as it does for the Go tests (2 doubles them).

type Env = Record<string, string | undefined>;
// Node's process, without pulling Node's types into the app's tsconfig.
const nodeEnv = (): Env => (globalThis as { process?: { env: Env } }).process?.env ?? {};

/** HADRON_TEST_WAIT_SCALE as a positive number, or 1. */
export function waitScale(env: Env = nodeEnv()): number {
  const f = Number.parseFloat(env.HADRON_TEST_WAIT_SCALE ?? "");
  return Number.isFinite(f) && f > 0 ? f : 1;
}

/** How long one test (or hook) may run. */
export const TEST_TIMEOUT_MS = 30_000;
/** How long findBy* and waitFor wait for the page. */
export const ASYNC_UTIL_TIMEOUT_MS = 10_000;

export const scaled = (ms: number, env?: Env) => Math.round(ms * waitScale(env));
