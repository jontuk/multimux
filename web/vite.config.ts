/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": { target: "https://localhost:8686", secure: false },
      "/healthz": { target: "https://localhost:8686", secure: false },
      "/ws": { target: "https://localhost:8686", secure: false, ws: true },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test-setup.ts"],
    globals: true,
  },
});
