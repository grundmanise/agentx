import { defineConfig, devices } from "@playwright/test";

import { telemetryHost, telemetryKey } from "./e2e/harness/telemetry-host.ts";

const port = 1420;
// The dev server of the `telemetry` project, built with a telemetry host and key, which no other
// dev server has: e2e/telemetry.spec.ts intercepts that host.
const telemetryPort = 1425;
// To use a Chromium already on the machine instead of Playwright's download, point this at it.
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE;
const ci = Boolean(process.env.CI);

// End-to-end tests: Chromium drives the frontend on the Vite dev server.
export default defineConfig({
  forbidOnly: ci,
  fullyParallel: true,
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: executablePath === undefined ? {} : { executablePath },
      },
    },
    {
      name: "telemetry",
      testMatch: "telemetry.spec.ts",
      use: {
        ...devices["Desktop Chrome"],
        baseURL: `http://localhost:${telemetryPort}`,
        launchOptions: executablePath === undefined ? {} : { executablePath },
      },
    },
  ],
  reporter: ci ? [["list"], ["github"]] : [["list"]],
  retries: 0,
  testDir: "e2e",
  // One locale and time zone on every machine, so a date the app formats reads the same in a test.
  use: {
    baseURL: `http://localhost:${port}`,
    locale: "en-US",
    timezoneId: "UTC",
    trace: "on-first-retry",
  },
  webServer: [
    {
      command: "pnpm dev",
      reuseExistingServer: !ci,
      url: `http://localhost:${port}`,
    },
    {
      command: `pnpm dev --port ${telemetryPort}`,
      env: {
        VITE_CACHE_DIR: "node_modules/.vite-telemetry",
        VITE_TELEMETRY_HOST: telemetryHost,
        VITE_TELEMETRY_KEY: telemetryKey,
      },
      reuseExistingServer: !ci,
      url: `http://localhost:${telemetryPort}`,
    },
  ],
});
