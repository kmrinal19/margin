// Playwright config for margin's comment UI. Launches the real margin binary
// against the repo's docs/ with a throwaway data dir.
const { defineConfig, devices } = require("@playwright/test");

const PORT = 8866;

module.exports = defineConfig({
  testDir: "./tests",
  timeout: 30000,
  expect: { timeout: 7000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    actionTimeout: 7000,
    trace: "retain-on-failure",
  },
  webServer: {
    command: `rm -rf ./e2e-data && ../margin serve --port ${PORT} --docs ../docs --data ./e2e-data`,
    url: `http://127.0.0.1:${PORT}/healthz`,
    reuseExistingServer: false,
    timeout: 20000,
    stdout: "pipe",
    stderr: "pipe",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
