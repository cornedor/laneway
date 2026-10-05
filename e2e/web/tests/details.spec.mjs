import { test, expect, openIssue, pick } from '../fixtures.mjs';

const iss = page => page.locator('.iss');
// section is the panel's part under the heading name.
const section = (page, name) => page.locator('.iss .sec-head', { has: page.locator('h3', { hasText: new RegExp('^' + name + '$') }) }).locator('xpath=..');
// reload reloads the page and waits for the panel to be DEMO-4's again.
async function reload(page) {
  await page.reload();
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-4');
}

test.beforeEach(async ({ page, app }) => {
  await openIssue(page, app, 'DEMO-4');
});

test('L links another issue, both ends show it', async ({ page }) => {
  await page.keyboard.press('L');
  await pick(page, 'blocks');
  await pick(page, 'DEMO-9', /DEMO-9\b/);
  await expect(section(page, 'Links')).toContainText('DEMO-9');
  await reload(page);
  await expect(section(page, 'Links')).toContainText('DEMO-9');
  await page.locator('.card', { hasText: 'Double click on Pay' }).click();
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-9');
  await expect(section(page, 'Links')).toContainText('DEMO-4');
});

test('a subtask, from the actions', async ({ page }) => {
  await expect(section(page, 'Child issues')).toContainText('1/3');
  await page.keyboard.press('A');
  await pick(page, 'New subtask');
  const dialog = page.getByRole('dialog', { name: 'Create issue' });
  await expect(dialog.getByRole('combobox').nth(1)).toHaveValue('Sub-task');
  await dialog.getByPlaceholder(/^Summary/).fill('Guest e-mail validation');
  await dialog.getByRole('button', { name: 'Create', exact: true }).click();
  // The new subtask opens, under DEMO-4; its parent link goes back.
  await expect(page.locator('.iss .iss-title')).toHaveText('Guest e-mail validation');
  const parent = page.locator('.iss .fld[data-field="parent"]');
  await expect(parent).toContainText('DEMO-4');
  await parent.getByRole('link', { name: 'DEMO-4' }).click();
  await expect(page.locator('.iss .iss-key')).toHaveText('DEMO-4');
  await expect(section(page, 'Child issues')).toContainText('Guest e-mail validation');
  await expect(section(page, 'Child issues')).toContainText('1/4');
  await reload(page);
  await expect(section(page, 'Child issues')).toContainText('Guest e-mail validation');
});

test('w logs work, the work log has it', async ({ page }) => {
  await page.keyboard.press('w');
  const dialog = page.getByRole('dialog', { name: 'Log work on DEMO-4' });
  await expect(dialog.getByRole('textbox', { name: 'Time' })).toBeFocused();
  await dialog.getByRole('textbox', { name: 'Time' }).fill('1h 30m');
  await dialog.getByRole('textbox', { name: 'Comment' }).fill('Pairing on the guest flow');
  await dialog.getByRole('button', { name: 'Log work' }).click();
  await expect(dialog).toHaveCount(0);
  await page.keyboard.press('3');
  await page.getByRole('button', { name: /^Work log/ }).click();
  await expect(iss(page)).toContainText('Pairing on the guest flow');
  await reload(page);
  await page.keyboard.press('3');
  await page.getByRole('button', { name: /^Work log/ }).click();
  await expect(iss(page)).toContainText('Pairing on the guest flow');
});

test('T starts a timer and T again stops it into a work log', async ({ page }) => {
  const chip = page.locator('.timer-chip');
  await page.keyboard.press('T');
  await expect(chip).toContainText('DEMO-4');
  await reload(page);
  await expect(chip).toContainText('DEMO-4'); // the state file keeps it
  await page.keyboard.press('T');
  const dialog = page.getByRole('dialog', { name: 'Log work on DEMO-4' });
  await expect(dialog.getByRole('textbox', { name: 'Time' })).toHaveValue(/m$/);
  await dialog.getByRole('textbox', { name: 'Comment' }).fill('Timed');
  await dialog.getByRole('button', { name: 'Log work' }).click();
  await expect(chip).toBeHidden();
  await page.keyboard.press('3');
  await page.getByRole('button', { name: /^Work log/ }).click();
  await expect(iss(page)).toContainText('Timed');
});

test('an edit shows in the history', async ({ page }) => {
  await page.keyboard.press('p');
  await pick(page, 'Low');
  await expect(page.locator('.iss .fld[data-field="priority"]')).toContainText('Low');
  await page.keyboard.press('3');
  await page.getByRole('button', { name: /^Changes/ }).click();
  await expect(iss(page)).toContainText(/priority\s*High\s*→\s*Low/);
});

test('a file uploaded from the actions', async ({ page }) => {
  const chooser = page.waitForEvent('filechooser');
  await page.keyboard.press('A');
  await pick(page, 'Upload files');
  await (await chooser).setFiles({ name: 'receipt.txt', mimeType: 'text/plain', buffer: Buffer.from('total 12.99') });
  await expect(section(page, 'Attachments')).toContainText('receipt.txt');
  await reload(page);
  await expect(section(page, 'Attachments')).toContainText('receipt.txt');
});
