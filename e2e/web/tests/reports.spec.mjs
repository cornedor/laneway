import { test, expect } from '../fixtures.mjs';

test('each kind draws', async ({ page, app }) => {
  await page.goto(new URL('#/reports', app.url).href);
  const kinds = { Burnup: 'burnup', 'Cumulative flow': 'cfd', Velocity: 'velocity', 'Cycle time': 'cycle', Retro: 'retro', Releases: 'releases' };
  for (const [name, kind] of Object.entries(kinds)) {
    await page.locator('#toolbar').getByText(name, { exact: true }).click();
    await expect(page).toHaveURL(new RegExp('/reports/' + kind + '\\b'));
    await expect(page.locator('#view .empty')).toHaveCount(0);
  }
});
