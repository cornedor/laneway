import { test, expect, inLane, selectCard } from '../fixtures.mjs';

const lane = (page, name) => page.locator('.bd-lane', { has: page.locator('.bd-lane-name', { hasText: name }) });
const card = (page, key) => page.locator('.card', { hasText: key });

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await expect(card(page, 'DEMO-5')).toBeVisible();
});

test('the active sprint in lanes', async ({ page }) => {
  for (const name of ['TO DO', 'IN PROGRESS', 'IN REVIEW', 'DONE']) await expect(lane(page, name)).toBeVisible();
  await expect(lane(page, 'IN PROGRESS').locator('.card:visible')).toHaveCount(2);
  await expect(inLane(page, 'IN PROGRESS', 'DEMO-4')).toBeVisible();
  await expect(page.locator('.bd-stats')).toHaveText('10 issues · 25 pts');
});

test('L moves a card a lane on, and Jira keeps it', async ({ page }) => {
  await selectCard(page, 'DEMO-5');
  await page.keyboard.press('L');
  await expect(inLane(page, 'IN PROGRESS', 'DEMO-5')).toBeVisible();
  await page.reload();
  await expect(inLane(page, 'IN PROGRESS', 'DEMO-5')).toBeVisible();
  await expect(inLane(page, 'TO DO', 'DEMO-5')).toHaveCount(0);
});

test('the filter and a quick filter narrow the cards', async ({ page }) => {
  await page.locator('.bd-filter').fill('address');
  await expect(page.locator('.bd-lanes .card:visible')).toHaveCount(2);
  await page.locator('.bd-filter').fill('');
  await expect(page.locator('.bd-lanes .card:visible')).toHaveCount(10);
  await page.locator('.fchip', { hasText: 'Bugs' }).click();
  await expect(page.locator('.bd-lanes .card:visible')).toHaveCount(3);
  for (const c of await page.locator('.bd-lanes .card:visible').all()) await expect(c.locator('.ctype')).toHaveAttribute('title', 'Bug');
});

test('the assignee picker: a name picks it alone, the box ticks', async ({ page }) => {
  const dialog = page.getByRole('dialog');
  const row = name => dialog.locator('.pick-row', { hasText: name });
  await page.keyboard.press('A');
  await row('Mira Jansen').locator('.pick-label').click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.fchip', { hasText: 'Mira Jansen' })).toBeVisible();
  await page.keyboard.press('A');
  await row('Priya Nair').locator('.check').click();
  await expect(dialog).toBeVisible();
  await row('Sam Okafor').locator('.check').click();
  await dialog.getByRole('button', { name: 'Apply' }).click();
  await expect(page.locator('.fchip', { hasText: 'Mira Jansen +2' })).toBeVisible();
});

test('a list column dragged onto another moves there, and stays', async ({ page }) => {
  await page.keyboard.press('t');
  const head = page.locator('.bd-lhead');
  await expect(head.locator('[data-col]').nth(1)).toHaveAttribute('data-col', 'key');
  await head.locator('[data-col=status]').dragTo(head.locator('[data-col=key]'), { targetPosition: { x: 2, y: 5 } });
  await expect(head.locator('[data-col]').nth(1)).toHaveAttribute('data-col', 'status');
  await expect(page.locator('.lrow', { hasText: 'DEMO-5' }).locator('> span').nth(1)).toHaveClass(/l-status/);
  await page.reload();
  await expect(page.locator('.bd-lhead [data-col]').nth(1)).toHaveAttribute('data-col', 'status');
});

test('a column drops anywhere over the header, below a label or in a gap', async ({ page }) => {
  await page.keyboard.press('t');
  const head = page.locator('.bd-lhead'), cols = () => head.locator('[data-col]');
  const box = async c => head.locator(`[data-col=${c}]`).boundingBox();
  const hb = await head.boundingBox(), key = await box('key');
  // under the key label, near the header's bottom edge
  await head.locator('[data-col=status]').dragTo(head, { targetPosition: { x: key.x - hb.x + 4, y: hb.height - 3 } });
  await expect(cols().nth(1)).toHaveAttribute('data-col', 'status');
  // in the gap just right of the key: after it
  const k2 = await box('key');
  await head.locator('[data-col=priority]').dragTo(head, { targetPosition: { x: k2.x + k2.width - hb.x + 3, y: 3 } });
  await expect(cols().nth(3)).toHaveAttribute('data-col', 'priority');
});

test('a phone keeps the key, summary and status, whatever the order', async ({ page }) => {
  await page.keyboard.press('t');
  const head = page.locator('.bd-lhead');
  await head.locator('[data-col=updated]').dragTo(head.locator('[data-col=key]'), { targetPosition: { x: 2, y: 5 } });
  await page.setViewportSize({ width: 400, height: 800 });
  await expect(head.locator('[data-col=updated]')).toBeHidden();
  for (const c of ['key', 'summary', 'status']) await expect(head.locator(`[data-col=${c}]`)).toBeVisible();
});
