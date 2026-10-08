import { test, expect } from '../fixtures.mjs';

test('2 shows the mentions, enter opens the thread', async ({ page, app }) => {
  await page.goto(new URL('#/inbox', app.url).href);
  const key = k => page.locator('#view').getByText(k, { exact: true }).first();
  await expect(key('DEMO-11')).toBeVisible();
  await page.keyboard.press('2');
  await expect(key('DEMO-11')).toBeHidden();
  await expect(key('DEMO-9')).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-9');
});

test('e marks a thread done, it leaves the inbox', async ({ page, app }) => {
  await page.goto(new URL('#/inbox', app.url).href);
  const key = k => page.locator('#view').getByText(k, { exact: true }).first();
  await expect(key('DEMO-9')).toBeVisible();
  await page.keyboard.press('e');
  await expect(key('DEMO-9')).toBeHidden();
  await page.reload();
  await expect(key('DEMO-11')).toBeVisible();
  await expect(key('DEMO-9')).toBeHidden();
});
