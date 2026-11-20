/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { scaled, TEST_TIMEOUT_MS } from "./src/test/waits";

// Generous so a loaded Mac does not fail a test that only ran slowly; see src/test/waits.ts.
const timeout = scaled(TEST_TIMEOUT_MS);

export default defineConfig({
  plugins: [react()],
  // api.test.ts reads ../internal/actions/testdata/actions.json, which sits outside the frontend root.
  server: { fs: { allow: [".."] } },
  test: { environment: "jsdom", globals: true, setupFiles: ["src/test/setup.ts"], css: true, testTimeout: timeout, hookTimeout: timeout },
});
