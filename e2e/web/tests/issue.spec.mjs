import { test, expect } from '../fixtures.mjs';

const lane = (page, name) => page.locator('.bd-lane', { has: page.locator('.bd-lane-name', { hasText: name }) });
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
  await expect(lane(page, 'DONE')).toContainText('DEMO-4');
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
