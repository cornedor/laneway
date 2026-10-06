import { test, expect, pick, inLane, selectCard } from '../fixtures.mjs';

const card = (page, key) => page.locator('.bd-lanes .card', { has: page.locator('.ckey', { hasText: new RegExp('^' + key + '$') }) });

// x marks DEMO-5 and DEMO-9 (the first two in To Do), X edits both.
async function markTwo(page, app) {
  await page.goto(app.url);
  await selectCard(page, 'DEMO-5');
  await page.keyboard.press('x');
  await page.keyboard.press('x');
  await expect(page.locator('.bd-lanes .card.mark')).toHaveCount(2);
  await page.keyboard.press('X');
  await expect(page.getByRole('dialog')).toContainText('Edit 2 issues: DEMO-5, DEMO-9');
}

test('bulk: a status for two cards, and u undoes it', async ({ page, app }) => {
  await markTwo(page, app);
  await pick(page, 'Status');
  await pick(page, 'In Review');
  for (const k of ['DEMO-5', 'DEMO-9']) await expect(inLane(page, 'IN REVIEW', k)).toBeVisible();
  await page.keyboard.press('u');
  for (const k of ['DEMO-5', 'DEMO-9']) await expect(inLane(page, 'TO DO', k)).toBeVisible();
  await page.reload();
  for (const k of ['DEMO-5', 'DEMO-9']) await expect(inLane(page, 'TO DO', k)).toBeVisible();
});

test('bulk: an assignee for two cards', async ({ page, app }) => {
  await markTwo(page, app);
  await pick(page, 'Assignee');
  await pick(page, 'Priya', /Priya Nair/);
  for (const k of ['DEMO-5', 'DEMO-9']) await expect(card(page, k).locator('.cav')).toContainText('PN');
  await page.reload();
  for (const k of ['DEMO-5', 'DEMO-9']) await expect(card(page, k).locator('.cav')).toContainText('PN');
});

// x shows every card's checkbox; a click on one marks it; the bar counts them and opens the bulk edit.
test('bulk: checkboxes and the selection bar', async ({ page, app }) => {
  await page.goto(app.url);
  await selectCard(page, 'DEMO-5');
  const bar = page.getByRole('toolbar', { name: 'Selected issues' });
  await expect(bar).toBeHidden();
  await page.keyboard.press('x');
  await expect(bar).toContainText('1 selected');
  await card(page, 'DEMO-9').locator('.chk').click();
  await expect(card(page, 'DEMO-9')).toHaveClass(/\bmark\b/);
  await expect(bar).toContainText('2 selected');
  await bar.getByRole('button', { name: /Edit/ }).click();
  await expect(page.getByRole('dialog')).toContainText('Edit 2 issues: DEMO-5, DEMO-9');
  await page.keyboard.press('Escape');
  await bar.getByRole('button', { name: 'Clear the selection' }).click();
  await expect(bar).toBeHidden();
  await expect(page.locator('.bd-lanes .card.mark')).toHaveCount(0);
});

// In the list a row's checkbox cell marks it without opening the issue.
test('bulk: the list checkbox', async ({ page, app }) => {
  await page.goto(app.url);
  await selectCard(page, 'DEMO-5');
  await page.keyboard.press('t');
  const row = page.locator('.bd-list .lrow[data-key="DEMO-9"]');
  await row.hover();
  await row.locator('.chk').click();
  await expect(row).toHaveClass(/\bmark\b/);
  await expect(page.getByRole('toolbar', { name: 'Selected issues' })).toContainText('1 selected');
  await expect(page.locator('#panel')).not.toContainText('DEMO-9');
});
