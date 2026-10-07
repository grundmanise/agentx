/// <reference types="vitest/config" />
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Tauri serves the dev build from a fixed port and expects it to fail rather than move.
export default defineConfig({
  build: { outDir: "dist", target: "safari16" },
  clearScreen: false,
  envPrefix: ["VITE_", "TAURI_ENV_"],
  plugins: [react(), tailwindcss()],
  server: { port: 1420, strictPort: true },
  test: {
    css: false,
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
