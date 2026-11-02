import {defineConfig, devices} from '@playwright/test';

export default defineConfig({
    testDir: './e2e',
    testMatch: '**/*.spec.ts',
    workers: 1,
    retries: 0,
    forbidOnly: !!process.env.CI,
    updateSnapshots: 'none',
    expect: {toHaveScreenshot: {maxDiffPixels: 0, threshold: 0.2}},
    reporter: [['list'], ['html', {open: 'never'}]],
    use: {
        ...devices['Desktop Chrome'],
        baseURL: 'http://127.0.0.1:4179',
        viewport: {width: 1200, height: 900},
        locale: 'ja-JP',
        timezoneId: 'Asia/Tokyo',
        serviceWorkers: 'block',
        trace: 'retain-on-failure',
        screenshot: 'only-on-failure',
    },
    webServer: {
        command: 'npm run preview -- --host 127.0.0.1 --port 4179 --strictPort',
        url: 'http://127.0.0.1:4179',
        reuseExistingServer: false,
        timeout: 30_000,
    },
});
