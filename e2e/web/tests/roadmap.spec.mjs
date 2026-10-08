import { test, expect } from '../fixtures.mjs';

const row = (page, key) => page.locator('.rm-row', { has: page.locator('.rm-key', { hasText: new RegExp('^' + key + '$') }) });

test.beforeEach(async ({ page, app }) => {
  await page.goto(new URL('#/roadmap', app.url).href);
  await expect(row(page, 'DEMO-1')).toBeVisible();
});

test('F filters the epics', async ({ page }) => {
  await page.keyboard.press('F');
  await page.keyboard.type('invoice');
  await expect(row(page, 'DEMO-3')).toBeVisible();
  await expect(row(page, 'DEMO-1')).toHaveCount(0);
});

test('L moves a bar later, and Jira keeps the dates', async ({ page }) => {
  const bar = row(page, 'DEMO-2').locator('.rm-bar');
  const was = await bar.getAttribute('title');
  await row(page, 'DEMO-2').locator('.rm-label').click();
  await page.keyboard.press('L');
  await expect(page.getByRole('status')).toContainText('Saved DEMO-2');
  const now = await bar.getAttribute('title');
  expect(now).not.toBe(was);
  await page.reload();
  await expect(row(page, 'DEMO-2').locator('.rm-bar')).toHaveAttribute('title', now);
});

test('enter opens the epic in the panel', async ({ page }) => {
  await row(page, 'DEMO-3').locator('.rm-label').click();
  await page.keyboard.press('Enter');
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-3');
});
