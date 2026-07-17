/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Daemon the dev server proxies to. Override when the default port is taken
// (e.g. a real install already listens on 8686):
//   MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev
const target = process.env.MULTIMUX_DEV_TARGET ?? "https://localhost:8686";

export default defineConfig({
  plugins: [react()],
  build: {
    chunkSizeWarningLimit: 1024, // KiB; xterm alone pushes the main chunk past the 500 default
  },
  server: {
    proxy: {
      "/api": { target, secure: false },
      "/healthz": { target, secure: false },
      "/ws": { target, secure: false, ws: true },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test-setup.ts"],
    globals: true,
  },
});
