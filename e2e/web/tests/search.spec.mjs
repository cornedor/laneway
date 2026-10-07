import { test, expect, palette } from '../fixtures.mjs';

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
});

test('/ finds an issue and opens it', async ({ page }) => {
  await (await palette(page, '/', 'house number', /Address form loses the house number/)).click();
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-13');
});

test(': runs a command', async ({ page }) => {
  await (await palette(page, ':', 'roadmap', /Go to Roadmap/)).click();
  await expect(page).toHaveURL(/\/roadmap$/);
});
