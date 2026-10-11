import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests/browser",
  workers: 1,
  timeout: 90000,
  use: {
    headless: true,
    viewport: { width: 1440, height: 1060 },
    launchOptions: {
      args: [
        "--use-fake-device-for-media-stream",
        "--use-fake-ui-for-media-stream",
      ],
    },
    screenshot: process.env.ZWOICE_TEST_CF_KEY ? "off" : "only-on-failure",
    trace: process.env.ZWOICE_TEST_CF_KEY ? "off" : "retain-on-failure",
  },
  webServer: {
    command: "go run ./tests/harness.go",
    url: "http://127.0.0.1:18787",
    reuseExistingServer: !process.env.CI && !process.env.ZWOICE_TEST_CF_KEY,
    timeout: 120000,
  },
});
