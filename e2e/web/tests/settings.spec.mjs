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
