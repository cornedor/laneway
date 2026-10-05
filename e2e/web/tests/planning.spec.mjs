import { test, expect, pick } from '../fixtures.mjs';

const head = (page, name) => page.locator('.pl-head', { has: page.locator('.pl-name', { hasText: new RegExp('^' + name + '$') }) });
const row = (page, key) => page.locator('.lrow', { has: page.locator('.l-key', { hasText: new RegExp('^' + key + '$') }) });

// select puts the cursor on key's row: a click opens it beside the list,
// esc gives the keys back to the list.
async function select(page, key) {
  await row(page, key).click();
  await expect(page.locator('.iss .iss-key')).toHaveText(key);
  await page.keyboard.press('Escape');
  await expect(page.locator('.iss')).toHaveCount(0);
}

test.beforeEach(async ({ page, app }) => {
  await page.goto(new URL('#/planning', app.url).href);
  await expect(row(page, 'DEMO-17')).toBeVisible();
});

test('m moves a backlog issue into the next sprint and back', async ({ page }) => {
  await expect(head(page, 'Sprint 13')).toContainText('3 issues');
  await select(page, 'DEMO-17');
  await page.keyboard.press('m');
  await pick(page, 'Sprint 13', /Sprint 13/);
  await expect(head(page, 'Sprint 13')).toContainText('4 issues');
  await expect(head(page, 'Backlog')).toContainText('3 issues');
  await page.reload();
  await expect(head(page, 'Sprint 13')).toContainText('4 issues');
  await select(page, 'DEMO-17');
  await page.keyboard.press('m');
  await pick(page, 'Backlog');
  await expect(head(page, 'Sprint 13')).toContainText('3 issues');
  await expect(head(page, 'Backlog')).toContainText('4 issues');
});

test('K ranks an issue up', async ({ page }) => {
  const above = async (a, b) => (await row(page, a).boundingBox()).y < (await row(page, b).boundingBox()).y;
  expect(await above('DEMO-18', 'DEMO-19')).toBe(true);
  await select(page, 'DEMO-19');
  await page.keyboard.press('K');
  await expect.poll(() => above('DEMO-19', 'DEMO-18')).toBe(true);
  await page.reload();
  await expect(row(page, 'DEMO-19')).toBeVisible();
  expect(await above('DEMO-19', 'DEMO-18')).toBe(true);
});

test('a sprint goal', async ({ page }) => {
  await head(page, 'Sprint 13').getByRole('button', { name: 'Edit' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/goal/i).fill('Invoices out of the door');
  await dialog.getByRole('button', { name: /save|ok/i }).click();
  await expect(head(page, 'Sprint 13')).toContainText('Invoices out of the door');
  await page.reload();
  await expect(head(page, 'Sprint 13')).toContainText('Invoices out of the door');
});

test('complete the active sprint, start the next', async ({ page }) => {
  await head(page, 'Sprint 12 - Checkout').getByRole('button', { name: 'Complete' }).click();
  await page.getByRole('dialog').getByRole('button', { name: /complete/i }).click();
  await expect(head(page, 'Sprint 12 - Checkout')).toHaveCount(0);
  await head(page, 'Sprint 13').getByRole('button', { name: 'Start' }).click();
  await page.getByRole('dialog').getByRole('button', { name: /start/i }).click();
  await expect(head(page, 'Sprint 13')).toContainText('active');
  // The open issues of the sprint closed went on: the board is Sprint 13's.
  await page.goto(page.url().replace(/#.*/, '#/board'));
  await expect(page.locator('.crumb', { hasText: 'Sprint 13' })).toBeVisible();
  await expect(page.locator('.bd-lanes .card', { hasText: 'DEMO-14' })).toBeVisible();
});
