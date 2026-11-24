import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Tauri serves the dev build from a fixed port and expects it to fail rather than move.
export default defineConfig(({ command }) => ({
  build: { outDir: "dist", target: "safari16" },
  clearScreen: false,
  // Names the build, so that a launch reads only the inventory its own build kept
  // (src/store/launch-cache.ts). The dev server keeps one name: what it kept outlives a restart.
  define: {
    "import.meta.env.APP_BUILD": JSON.stringify(
      command === "build" ? Date.now().toString(36) : "dev"
    ),
  },
  envPrefix: ["VITE_", "TAURI_ENV_"],
  // The Tauri API is first imported by the CLI transport, which the app does not load at start.
  // Bundled up front, so that the dev server does not find it mid-session and reload every page,
  // which would interrupt the end-to-end tests that load the transport. idb-keyval likewise: an
  // end-to-end test reads the kept reads' index with it. And the telemetry SDK, which the app
  // imports only once the telemetry setting is on.
  optimizeDeps: {
    include: [
      "@tauri-apps/api/core",
      "@tauri-apps/api/event",
      "idb-keyval",
      "posthog-js/dist/module.slim.no-external.js",
      "posthog-js/dist/extension-bundles.js",
      "posthog-js/dist/exception-autocapture.js",
    ],
  },
  // React Compiler, in Oxc's Rust port (oxc-transform-react), memoizes components and hooks at
  // build time. It skips any it cannot compile safely; oxlint's React Compiler rules
  // (ultracite/oxlint/react) report those.
  plugins: [react({ compiler: true }), tailwindcss()],
  server: { port: 1420, strictPort: true },
}));
