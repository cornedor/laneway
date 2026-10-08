import { test, expect, palette } from '../fixtures.mjs';

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
});

test('? lists the keys pressable here', async ({ page }) => {
  await page.keyboard.press('?');
  const help = page.getByRole('dialog', { name: 'Keyboard' });
  await expect(help.locator('h3', { hasText: /^Board$/ })).toBeVisible();
  await expect(help.locator('h3', { hasText: /^Global$/ })).toBeVisible();
  await expect(help.locator('dd', { hasText: 'next card' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(help).toHaveCount(0);
});

test('g t takes the next theme, and keeps it', async ({ page }) => {
  const theme = () => page.evaluate(() => document.documentElement.dataset.theme || 'auto');
  const was = await theme();
  await page.keyboard.press('g');
  await page.keyboard.press('t');
  await expect.poll(theme).not.toBe(was);
  const now = await theme();
  await page.reload();
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
  expect(await theme()).toBe(now);
});

test('g g jumps to an issue by key', async ({ page }) => {
  await page.keyboard.press('g');
  await (await palette(page, 'g', '13', /DEMO-13/)).click();
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-13');
  await expect(page).toHaveURL(/issue=DEMO-13/);
});

test('g and a view key go there', async ({ page }) => {
  for (const [key, url] of [['p', /\/planning/], ['m', /\/roadmap/], ['i', /\/inbox/], ['b', /\/board/]]) {
    await page.keyboard.press('g');
    await page.keyboard.press(key);
    await expect(page).toHaveURL(url);
  }
});
