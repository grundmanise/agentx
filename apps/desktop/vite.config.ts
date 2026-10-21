import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Tauri serves the dev build from a fixed port and expects it to fail rather than move.
export default defineConfig({
  build: { outDir: "dist", target: "safari16" },
  clearScreen: false,
  envPrefix: ["VITE_", "TAURI_ENV_"],
  // The Tauri API is first imported by the CLI transport, which the app does not load at start.
  // Bundled up front, so that the dev server does not find it mid-session and reload every page,
  // which would interrupt the end-to-end tests that load the transport.
  optimizeDeps: { include: ["@tauri-apps/api/core", "@tauri-apps/api/event"] },
  plugins: [react(), tailwindcss()],
  server: { port: 1420, strictPort: true },
});
