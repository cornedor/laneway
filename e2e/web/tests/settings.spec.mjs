import { test, expect } from '../fixtures.mjs';

// Settings > Site: the projects the picker lists first, picked from the site's.
test('projects are removed and picked again', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const row = page.getByRole('group', { name: 'Projects' });
  await expect(row.locator('.st-proj')).toHaveText(['DEMO×']);
  await row.getByRole('button', { name: 'Remove DEMO' }).click();
  await expect(row).toContainText('none');
  await row.getByRole('button', { name: 'Edit' }).click();
  await page.getByRole('option', { name: 'DEMO Demo Shop' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Apply' }).click();
  await expect(row.locator('.st-proj')).toHaveText(['DEMO×']);
});

// Restart serves the app again and the page comes back by itself.
test('restart comes back', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const reloaded = page.waitForEvent('load');
  await page.getByRole('group', { name: 'Restart' }).getByRole('button', { name: 'Restart' }).click();
  await reloaded;
  await expect(page.getByRole('group', { name: 'Projects' })).toBeVisible();
});

// Keys sit folded by group: a click opens one, the filter finds a key in a folded one.
test('key groups fold', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const keys = page.getByRole('group', { name: 'Keyboard', exact: true });
  const global = keys.locator('.st-fold', { hasText: 'Global' });
  await expect(global).toBeVisible();
  await expect(keys.getByRole('group', { name: 'jump to issue by key' })).toHaveCount(0);
  await global.click();
  await expect(keys.getByRole('group', { name: 'jump to issue by key' })).toBeVisible();
  await expect(global.locator('xpath=following-sibling::*[1]')).not.toHaveClass(/st-fold/); // its keys right under it
  await global.click();
  await expect(keys.getByRole('group', { name: 'jump to issue by key' })).toHaveCount(0);
  await page.getByRole('searchbox', { name: 'Filter settings' }).fill('jump to issue by key');
  await expect(keys.getByRole('group', { name: 'jump to issue by key' })).toBeVisible();
});

// The sidebar lists the sections and jumps to one.
test('sidebar jumps to a section', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const nav = page.getByRole('navigation', { name: 'Settings sections' });
  await nav.getByRole('link', { name: 'Keyboard' }).click();
  await expect(nav.getByRole('link', { name: 'Keyboard' })).toHaveClass(/on/);
  await expect(page.locator('.st-fold').first()).toBeInViewport();
});

// A lane layout is on every board it fits, or on the boards ticked.
test('a lane layout goes on every board it fits', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const g = page.getByRole('group', { name: 'lane_layouts' });
  await g.getByRole('button', { name: 'New layout' }).click();
  const every = g.getByRole('checkbox', { name: 'Every board it fits' }), board = g.getByRole('checkbox', { name: 'DEMO board' });
  await expect(every).toBeChecked();
  await board.check();
  await expect(every).not.toBeChecked();
  await every.check();
  await expect(board).not.toBeChecked();
  await every.uncheck();
  await expect(board).toBeChecked();
});

// A drag in the card designer keeps the page where it is: the drop redraws the row, not the scroll.
test('a card designer drag keeps the scroll', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const g = page.getByRole('group', { name: 'card_layout' });
  const chip = g.locator('.cd-tray .cd-chip').first(), top = g.locator('.cd-top .cd-chip');
  await expect(chip).toBeVisible();
  const n = await top.count();
  await chip.dragTo(g.locator('.cd-top'));
  await expect(top).toHaveCount(n + 1);
  await expect(g.locator('.cd-card')).toBeInViewport();
});

// A fold opens and shuts in place: the cursor stays on it, j steps into its keys or past them.
test('key groups fold by keyboard, the cursor stays', async ({ page, app }) => {
  await page.goto(new URL('#/settings', app.url).href);
  const keys = page.getByRole('group', { name: 'Keyboard', exact: true });
  const global = keys.locator('.st-fold', { hasText: 'Global' }), sel = page.locator('.st-row.sel');
  await global.click(); // opens, and selects it
  await expect(global).toHaveClass(/\bsel\b/);
  await page.keyboard.press('j');
  await expect(sel).toHaveAttribute('aria-label', 'command palette');
  await page.keyboard.press('k');
  await page.keyboard.press('Enter'); // shuts
  await expect(keys.getByRole('group', { name: 'command palette' })).toHaveCount(0);
  await expect(global).toHaveClass(/\bsel\b/);
  await page.keyboard.press('j');
  await expect(sel).toHaveClass(/\bst-fold\b/);
  await expect(sel).not.toContainText('Global');
});
