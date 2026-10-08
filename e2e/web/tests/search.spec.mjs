import { test, expect, palette } from '../fixtures.mjs';

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
});

test('/ finds an issue and opens it', async ({ page }) => {
  await (await palette(page, '/', 'house number', /Address form loses the house number/)).click();
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-13');
});

test('# lists a query: a hit opens among the rest, ctrl+enter lists them as a view', async ({ page }) => {
  const input = page.getByRole('textbox', { name: 'Palette' });
  const run = async () => {
    await page.keyboard.press('/');
    await input.pressSequentially('#key in (DEMO-3, DEMO-13)');
    await page.keyboard.press('Enter');
    await expect(page.getByRole('option', { name: /DEMO-13/ })).toBeVisible();
  };
  await run();
  await page.getByRole('option', { name: /DEMO-3 / }).click();
  await expect(page).toHaveURL(/sprint=jql/);
  const panelKey = page.locator('body.panel-focus .iss .iss-key');
  await expect(panelKey).toHaveText('DEMO-3');
  await page.keyboard.press(']');
  await expect(panelKey).toHaveText('DEMO-13');
  await page.reload();
  await expect(page.locator('.card', { hasText: 'DEMO-3' })).toBeVisible();
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toHaveCount(0);

  await page.keyboard.press('Escape');
  await run();
  await page.keyboard.press('Control+Enter');
  await expect(page.locator('.iss')).toHaveCount(0);
  await expect(page).toHaveURL(/sprint=jql/);
  await expect(page.locator('.card', { hasText: 'DEMO-13' })).toBeVisible();
});

test(': runs a command', async ({ page }) => {
  await (await palette(page, ':', 'roadmap', /Go to Roadmap/)).click();
  await expect(page).toHaveURL(/\/roadmap$/);
});
