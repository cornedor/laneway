import { test, expect, openIssue, pick, prompt } from '../fixtures.mjs';

// Each edit lands in the panel and on the card, and stays after a reload
// (the demo keeps writes for its life).
const field = (page, name) => page.locator(`.iss .fld[data-field="${name}"]`);
const card = page => page.locator('.bd-lanes .card', { hasText: 'DEMO-4' });

test.beforeEach(async ({ page, app }) => {
  await openIssue(page, app, 'DEMO-4');
});

test('assignee', async ({ page }) => {
  await page.keyboard.press('a');
  await pick(page, 'Priya', /Priya Nair/);
  await expect(field(page, 'assignee')).toContainText('Priya Nair');
  await expect(card(page).locator('.cav')).toContainText('PN');
  await page.reload();
  await expect(field(page, 'assignee')).toContainText('Priya Nair');
});

test('priority', async ({ page }) => {
  await page.keyboard.press('p');
  await expect(page.getByRole('option', { name: /High current/ })).toBeVisible();
  await pick(page, 'Lowest');
  await expect(field(page, 'priority')).toContainText('Lowest');
  await expect(card(page).locator('.cprio')).toHaveClass(/p4/);
  await page.reload();
  await expect(field(page, 'priority')).toContainText('Lowest');
});

test('points', async ({ page }) => {
  await page.keyboard.press('P');
  await prompt(page, '8');
  await expect(field(page, 'points')).toContainText('8');
  await expect(card(page).locator('.cpts')).toHaveText('8');
  await expect(page.locator('.bd-stats')).toHaveText('10 issues · 28 pts');
});

test('labels: one added, one taken off', async ({ page }) => {
  await page.keyboard.press('l');
  const input = page.locator('.pick-input');
  await expect(input).toBeFocused();
  await input.press('Space'); // the focused option, frontend: off
  await input.fill('checkout');
  await input.press('Tab'); // a new label
  await input.press('Enter');
  await expect(field(page, 'labels')).toContainText('checkout');
  await expect(field(page, 'labels')).not.toContainText('frontend');
  await expect(card(page).locator('.clabels')).toContainText('checkout');
  await page.reload();
  await expect(field(page, 'labels')).toContainText('checkout');
});

test('due date', async ({ page }) => {
  await field(page, 'due').click();
  await prompt(page, '2026-12-24');
  await expect(field(page, 'due')).toContainText('Dec 24');
  await expect(card(page).locator('.cdue')).toContainText('Dec 24');
});

test('description, in the markdown editor', async ({ page }) => {
  await page.keyboard.press('E');
  await expect(page.locator('.iss [data-placeholder^="Description"]')).toBeFocused();
  await page.keyboard.press('ControlOrMeta+End');
  await page.keyboard.press('Enter');
  await page.keyboard.type('- **Guests** keep their cart');
  await page.keyboard.press('ControlOrMeta+Enter');
  await expect(page.locator('.iss strong', { hasText: 'Guests' })).toBeVisible();
  await page.reload();
  await expect(page.locator('.iss strong', { hasText: 'Guests' })).toBeVisible();
  await expect(page.locator('.iss li', { hasText: 'keep their cart' })).toBeVisible();
});

test('a custom field: Team, under Fields', async ({ page }) => {
  await page.getByRole('button', { name: /^Fields/ }).click();
  const team = page.locator('.iss').getByRole('button', { name: /^Team/ });
  await team.click();
  const dialog = page.getByRole('dialog', { name: 'DEMO-4 Team' });
  await dialog.getByRole('combobox').selectOption('Platform');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(team).toContainText('Platform');
  await page.reload();
  await page.getByRole('button', { name: /^Fields/ }).click();
  await expect(team).toContainText('Platform');
});

test('a rich-text custom field: Test notes', async ({ page }) => {
  const head = page.locator('.iss .sec-head', { hasText: 'Test notes' });
  await head.getByRole('button', { name: 'Edit' }).click();
  await expect(page.locator('.iss [contenteditable]:focus')).toBeVisible();
  await page.keyboard.press('ControlOrMeta+End');
  await page.keyboard.type(' Also on iOS.');
  await page.keyboard.press('ControlOrMeta+Enter');
  await expect(page.locator('.iss')).toContainText('Also on iOS.');
  await page.reload();
  await expect(page.locator('.iss')).toContainText('Also on iOS.');
});
