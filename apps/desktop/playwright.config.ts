import { defineConfig, devices } from "@playwright/test";

const port = 1420;
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
  ],
  reporter: ci ? [["list"], ["github"]] : [["list"]],
  retries: 0,
  testDir: "e2e",
  use: { baseURL: `http://localhost:${port}`, trace: "on-first-retry" },
  webServer: {
    command: "pnpm dev",
    reuseExistingServer: !ci,
    url: `http://localhost:${port}`,
  },
});
