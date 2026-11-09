/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  // api.test.ts reads ../internal/actions/testdata/actions.json, which sits outside the frontend root.
  server: { fs: { allow: [".."] } },
  test: { environment: "jsdom", globals: true, setupFiles: ["src/test/setup.ts"], css: true },
});
