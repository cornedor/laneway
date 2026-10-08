import { test, expect } from '../fixtures.mjs';

// A board of 600 issues more, each test one kind of interaction: its INP
// (vitals-reporter.mjs) is that interaction's, held to a budget with
// INP_BUDGETS=1. A key held down repeats some 30 times a second, so moving
// gets 50ms; the rest 100ms, where a response stops feeling instant.
// CPU_SLOWDOWN=4 for a slower machine.
test.use({ bulk: +process.env.BULK || 600 });

const cards = page => page.locator('.bd-lanes .card:visible');
const sel = page => page.locator('.bd-lanes .card.sel .ckey');

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
});

// No click before: it opens the panel, an interaction of another budget.
test.describe('moving', () => {
  test.use({ inp: 50 });

  test('j walks a big lane', async ({ page }) => {
    for (let i = 0; i < 20; i++) await page.keyboard.press('j');
    await expect(sel(page)).toHaveCount(1);
  });

  test('h l walk the lanes', async ({ page }) => {
    await page.keyboard.press('j');
    await expect(sel(page)).toHaveText('DEMO-5');
    for (const k of 'lllhhh') await page.keyboard.press(k);
    await expect(sel(page)).toHaveText('DEMO-5');
  });

  test('j walks the planning list', async ({ page, app }) => {
    await page.goto(new URL('#/planning', app.url).href);
    await expect(page.locator('.lrow').first()).toBeVisible();
    for (let i = 0; i < 20; i++) await page.keyboard.press('j');
    await expect(page.locator('.lrow.sel')).toHaveCount(1);
  });
});

test.describe('the rest', () => {
  test.use({ inp: 100 });

  test('typing in the filter', async ({ page }) => {
    await page.locator('.bd-filter').pressSequentially('filler 12');
    await expect(cards(page).first()).toBeVisible();
  });

  test('a quick filter', async ({ page }) => {
    await page.locator('.fchip', { hasText: 'Bugs' }).click();
    await expect(cards(page).first()).toBeVisible();
    await page.locator('.fchip', { hasText: 'Bugs' }).click();
  });

  test('a card opens in the panel', async ({ page }) => {
    for (const key of ['DEMO-4', 'DEMO-1003', 'DEMO-7']) {
      await page.locator('.bd-lanes .card', { has: page.locator('.ckey', { hasText: new RegExp('^' + key + '$') }) }).click();
      await expect(page.locator('.iss .iss-key')).toHaveText(key);
    }
  });

  test('[ ] step through the panel', async ({ page }) => {
    await page.locator('.bd-lanes .card', { hasText: 'DEMO-4' }).click();
    await expect(page.locator('body.panel-focus .iss .iss-key')).toHaveText('DEMO-4');
    await page.keyboard.press(']');
    await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-6');
    await page.keyboard.press('[');
    await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-4');
  });

  test('L moves a card', async ({ page }) => {
    await page.keyboard.press('j');
    await expect(sel(page)).toHaveText('DEMO-5');
    await page.keyboard.press('L');
    await expect(page.locator('.bd-lane', { hasText: 'IN PROGRESS' }).locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
  });

  test('t switches to the list and back', async ({ page }) => {
    await page.keyboard.press('t');
    await expect(page.locator('.bd-lhead')).toBeVisible();
    await page.keyboard.press('t');
    await expect(page.locator('.bd-lhead')).toBeHidden();
  });

  test('typing in the search palette', async ({ page }) => {
    await page.keyboard.press('/');
    await page.getByRole('textbox', { name: 'Palette' }).pressSequentially('filler work 3');
    await expect(page.getByRole('option').first()).toBeVisible();
  });

  test('g p, g b: planning and back', async ({ page }) => {
    await page.keyboard.press('g');
    await page.keyboard.press('p');
    await expect(page.locator('.lrow').first()).toBeVisible();
    await page.keyboard.press('g');
    await page.keyboard.press('b');
    await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
  });

  test('a settings key group folds', async ({ page, app }) => {
    await page.goto(new URL('#/settings', app.url).href);
    const fold = page.getByRole('group', { name: 'Keyboard', exact: true }).locator('.st-fold', { hasText: 'Global' });
    await fold.click();
    await expect(page.getByRole('group', { name: 'command palette' })).toBeVisible();
    await fold.click();
    await expect(page.getByRole('group', { name: 'command palette' })).toHaveCount(0);
  });

  test('settings show the advanced options', async ({ page, app }) => {
    await page.goto(new URL('#/settings', app.url).href);
    await expect(page.getByRole('group', { name: 'lane_layouts' })).toBeVisible();
    await page.keyboard.press('a');
    await expect(page.getByRole('group', { name: 'panel_width' })).toBeVisible();
    await page.keyboard.press('a');
    await expect(page.getByRole('group', { name: 'panel_width' })).toHaveCount(0);
  });
});
