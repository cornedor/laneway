import { test, expect, inLane } from '../fixtures.mjs';

const panel = page => page.locator('.iss');

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await page.locator('.card', { hasText: 'DEMO-4' }).click();
  await expect(panel(page).locator('.iss-title')).toHaveText('Checkout without an account');
});

test('the panel shows the issue', async ({ page }) => {
  const p = panel(page);
  await expect(p.locator('.iss-key')).toHaveText('DEMO-4');
  await expect(p.locator('.fields')).toContainText('Jamie Rivers');
  await expect(p.locator('.fields')).toContainText('Sprint 12 - Checkout');
  await p.locator('.tab', { hasText: 'Comments' }).click();
  await expect(p.locator('.iss-scroll')).not.toBeEmpty();
});

test('a comment', async ({ page }) => {
  await page.keyboard.press('c');
  await page.keyboard.type('Ship it after the review');
  await page.keyboard.press('Control+Enter');
  await expect(panel(page)).toContainText('Ship it after the review');
});

test('s moves it to Done, on the board too', async ({ page }) => {
  await page.keyboard.press('s');
  await expect(page.locator('.pick-input')).toBeFocused();
  await page.locator('.pick-input').fill('Done');
  await page.keyboard.press('Enter');
  await expect(panel(page).locator('.iss-sub')).toContainText('Done');
  await expect(inLane(page, 'DONE', 'DEMO-4')).toBeVisible();
});

test('e renames it', async ({ page }) => {
  await page.keyboard.press('e');
  const input = page.getByRole('dialog').locator('input');
  await expect(input).toBeFocused();
  await input.fill('Checkout as a guest');
  await input.press('Enter');
  await expect(panel(page).locator('.iss-title')).toHaveText('Checkout as a guest');
  await expect(page.locator('.card', { hasText: 'DEMO-4' })).toContainText('Checkout as a guest');
});

test('e edits your own comment, d deletes it', async ({ page }) => {
  await page.keyboard.press('c');
  await page.keyboard.type('Ship it after the review');
  await page.keyboard.press('Control+Enter');
  const mine = panel(page).locator('article.cm', { hasText: 'Ship it after the review' });
  await mine.click();
  await page.keyboard.press('e');
  await expect(panel(page).locator('[contenteditable]:focus')).toBeVisible();
  await page.keyboard.press('ControlOrMeta+End');
  await page.keyboard.type(', on Friday');
  await page.keyboard.press('ControlOrMeta+Enter');
  await expect(mine).toContainText('Ship it after the review, on Friday');
  await mine.click();
  await page.keyboard.press('d');
  await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
  await expect(mine).toHaveCount(0);
  await page.reload();
  await panel(page).locator('.tab', { hasText: 'Comments' }).click();
  await expect(panel(page)).not.toContainText('Ship it after the review');
});

test('* pins it first among the commands, and it stays', async ({ page }) => {
  await page.keyboard.press('*');
  await page.keyboard.press('Escape');
  await page.reload();
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
  await page.keyboard.press(':');
  const first = page.getByRole('option').first();
  await expect(first).toContainText('★ DEMO-4');
  await first.click();
  await expect(panel(page).locator('.iss-key')).toHaveText('DEMO-4');
});

test('< widens the panel, and it stays', async ({ page }) => {
  const width = async () => (await page.locator('#panel').boundingBox()).width;
  const w0 = await width();
  await page.keyboard.press('<');
  await expect.poll(width).toBeGreaterThan(w0 + 50); // 5% of 1400
  const w1 = await width();
  await page.reload();
  await expect(panel(page).locator('.iss-key')).toHaveText('DEMO-4');
  expect(Math.abs(await width() - w1)).toBeLessThan(2);
});
