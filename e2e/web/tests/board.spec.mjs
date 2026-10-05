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
  for (const c of await page.locator('.bd-lanes .card:visible').all()) await expect(c.locator('.ctype')).toHaveClass(/t-bug/);
});
