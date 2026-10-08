import { test, expect, pick } from '../fixtures.mjs';

test('a logs work on the day, the day sums it', async ({ page, app }) => {
  await page.goto(new URL('#/work?tab=day', app.url).href);
  await expect(page.locator('#view')).toContainText('0m of 8h');
  await page.keyboard.press('a');
  await pick(page, 'DEMO-4', /DEMO-4\b/);
  const dialog = page.getByRole('dialog', { name: 'Log work on DEMO-4' });
  await dialog.getByRole('textbox', { name: 'Time' }).fill('2h');
  await dialog.getByRole('button', { name: 'Log work' }).click();
  await expect(page.locator('#view')).toContainText('2h of 8h');
  await expect(page.locator('#view')).toContainText('DEMO-4');
});

test('d hides the done issues', async ({ page, app }) => {
  await page.goto(new URL('#/work', app.url).href);
  const key = k => page.locator('#view').getByText(k, { exact: true });
  await expect(key('DEMO-21')).toBeVisible();
  await page.keyboard.press('d');
  await expect(key('DEMO-21')).toBeHidden();
  await expect(key('DEMO-4')).toBeVisible();
});
