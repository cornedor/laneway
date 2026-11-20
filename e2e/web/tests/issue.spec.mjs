import { test, expect, inLane } from '../fixtures.mjs';

const panel = page => page.locator('.iss');

test.beforeEach(async ({ page, app }) => {
  await page.goto(app.url);
  await page.locator('.card', { hasText: 'DEMO-4' }).click();
  await expect(panel(page).locator('.iss-title')).toHaveText('Checkout without an account');
});

test('the panel shows the issue', async ({ page }) => {
  const p = panel(page);
  await expect(p.locator('.iss-key')).toHaveText('DEMO-4');
  await expect(p.locator('.fields')).toContainText('Jamie Rivers');
  await expect(p.locator('.fields')).toContainText('Sprint 12 - Checkout');
  await p.locator('.tab', { hasText: 'Comments' }).click();
  await expect(p.locator('.iss-scroll')).not.toBeEmpty();
});

test('a comment', async ({ page }) => {
  await page.keyboard.press('c');
  await page.keyboard.type('Ship it after the review');
  await page.keyboard.press('Control+Enter');
  await expect(panel(page)).toContainText('Ship it after the review');
});

test('a comment Jira answered too late is not posted twice', async ({ page, app }) => {
  const sent = [];
  await page.route('**/api/issues/DEMO-4/comments', async route => {
    sent.push(route.request().postDataJSON());
    if (sent.length > 1) return route.continue();
    await route.fetch(); // stored, but the answer comes too late
    await route.fulfill({ status: 504, contentType: 'application/json', body: JSON.stringify({ error: 'jira: DEMO-4 timed out after 20s' }) });
  });
  await page.keyboard.press('c');
  await page.keyboard.type('Landed after all');
  await page.keyboard.press('Control+Enter');
  await expect(page.getByRole('alert')).toContainText('may have posted');
  await page.locator('.composer [role="textbox"]').click();
  await page.keyboard.press('Control+Enter');
  await expect(page.getByRole('status')).toContainText('already');
  expect(sent.map(b => !!b.Check)).toEqual([false, true]);
  await expect(panel(page).locator('article.cm', { hasText: 'Landed after all' })).toHaveCount(1);
  app.errors.splice(app.errors.findIndex(e => e.startsWith('504 POST')), 1); // the late answer
});

test('s moves it to Done, on the board too', async ({ page }) => {
  await page.keyboard.press('s');
  await expect(page.locator('.pick-input')).toBeFocused();
  await page.locator('.pick-input').fill('Done');
  await page.keyboard.press('Enter');
  await expect(panel(page).locator('.iss-sub')).toContainText('Done');
  await expect(inLane(page, 'DONE', 'DEMO-4')).toBeVisible();
});

test('e renames it', async ({ page }) => {
  await page.keyboard.press('e');
  const input = page.getByRole('dialog').locator('input');
  await expect(input).toBeFocused();
  await input.fill('Checkout as a guest');
  await input.press('Enter');
  await expect(panel(page).locator('.iss-title')).toHaveText('Checkout as a guest');
  await expect(page.locator('.card', { hasText: 'DEMO-4' })).toContainText('Checkout as a guest');
});

test('e edits your own comment, d deletes it', async ({ page }) => {
  await page.keyboard.press('c');
  await page.keyboard.type('Ship it after the review');
  await page.keyboard.press('Control+Enter');
  const mine = panel(page).locator('article.cm', { hasText: 'Ship it after the review' });
  await mine.click();
  await page.keyboard.press('e');
  await expect(panel(page).locator('[contenteditable]:focus')).toBeVisible();
  await page.keyboard.press('ControlOrMeta+End');
  await page.keyboard.type(', on Friday');
  await page.keyboard.press('ControlOrMeta+Enter');
  await expect(mine).toContainText('Ship it after the review, on Friday');
  await mine.click();
  await page.keyboard.press('d');
  await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
  await expect(mine).toHaveCount(0);
  await page.reload();
  await panel(page).locator('.tab', { hasText: 'Comments' }).click();
  await expect(panel(page)).not.toContainText('Ship it after the review');
});

test('* pins it first among the commands, and it stays', async ({ page }) => {
  await page.keyboard.press('*');
  await page.keyboard.press('Escape');
  await page.reload();
  await expect(page.locator('.card', { hasText: 'DEMO-5' })).toBeVisible();
  await page.keyboard.press(':');
  const first = page.getByRole('option').first();
  await expect(first).toContainText('★ DEMO-4');
  await first.click();
  await expect(panel(page).locator('.iss-key')).toHaveText('DEMO-4');
});

test('< widens the panel, and it stays', async ({ page }) => {
  const width = async () => (await page.locator('#panel').boundingBox()).width;
  const w0 = await width();
  await page.keyboard.press('<');
  await expect.poll(width).toBeGreaterThan(w0 + 50); // 5% of 1400
  const w1 = await width();
  await page.reload();
  await expect(panel(page).locator('.iss-key')).toHaveText('DEMO-4');
  expect(Math.abs(await width() - w1)).toBeLessThan(2);
});

test('alt+i lists the attachments with who added them, and it stays', async ({ page, app }) => {
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');
  for (const [name, mimeType, buffer] of [['shot.png', 'image/png', png], ['export.csv', 'text/csv', Buffer.from('a,b\n')]]) {
    const res = await page.request.post(app.url + '/api/issues/DEMO-4/attachments', { multipart: { file: { name, mimeType, buffer } } });
    expect(res.ok()).toBeTruthy();
  }
  await page.reload();
  const files = panel(page).locator('.sec-head', { hasText: 'Attachments' }).locator('..');
  await expect(files.locator('.thumb', { hasText: 'shot.png' })).toBeVisible();
  await expect(files.locator('.row-link', { hasText: 'export.csv' })).toContainText('Jamie Rivers');
  await panel(page).click({ position: { x: 5, y: 5 } });
  await page.keyboard.press('Alt+i');
  await expect(files.locator('.thumb')).toHaveCount(0);
  const row = files.locator('.row-link.file', { hasText: 'shot.png' });
  await expect(row).toContainText('Jamie Rivers');
  await expect(row.locator('time')).toBeVisible();
  await page.reload();
  await expect(files.locator('.row-link.file', { hasText: 'shot.png' })).toBeVisible();
  await expect(files.locator('.thumb')).toHaveCount(0);
  await files.getByRole('button', { name: 'Show as thumbnails' }).click();
  await expect(files.locator('.thumb', { hasText: 'shot.png' })).toBeVisible();
});

// Pictures load after the comments are shown and make them taller: the end stays the end.
test('the comments stay at the latest while their pictures load', async ({ page, app }) => {
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="600"><rect width="600" height="600" fill="teal"/></svg>';
  await page.route('**/api/attachments/slow-*', async route => {
    await new Promise(r => setTimeout(r, 800)); // past the frames it takes to size the comments
    await route.fulfill({ contentType: 'image/svg+xml', body: svg });
  });
  for (const n of [1, 2, 3]) {
    const res = await page.request.post(app.url + '/api/issues/DEMO-4/comments', { data: { Markdown: `Shot ${n}\n\n![shot](attachment:slow-${n})` } });
    expect(res.ok()).toBeTruthy();
  }
  await page.reload();
  const p = panel(page);
  await expect(p.locator('.iss-key')).toHaveText('DEMO-4');
  await p.locator('.tab', { hasText: 'Comments' }).click();
  const last = p.locator('.cm img.md-img').last();
  await expect.poll(() => last.evaluate(n => n.complete && n.naturalHeight)).toBeGreaterThan(0);
  const fromEnd = () => p.locator('.iss-scroll').evaluate(n => n.scrollHeight - n.scrollTop - n.clientHeight);
  await expect.poll(fromEnd).toBeLessThan(2);
  // scrolling yourself lets go of it
  await p.locator('.iss-scroll').hover();
  await page.mouse.wheel(0, -400);
  await expect.poll(fromEnd).toBeGreaterThan(100);
});

// laneway-server's change stream (the demo has none: this stands in for it): someone else's comment
// shows in the open issue without a reload.
test.describe('on laneway-server', () => {
  test.use({ flavour: 'laneway' });
  test("someone else's comment shows", async ({ page }) => {
    let tell;
    const told = new Promise(r => { tell = r; });
    await page.addInitScript(() => { window.SharedWorker = undefined; }); // page.route misses a shared worker's requests
    await page.route('**/api/site/events', async route => {
      const ch = await told;
      await route.fulfill({ contentType: 'text/event-stream', body: `retry: 60000\n\nevent: change\ndata: ${JSON.stringify(ch)}\n\n` });
    });
    await page.reload();
    await page.locator('.card', { hasText: 'DEMO-4' }).click();
    await expect(panel(page).locator('.iss-key')).toHaveText('DEMO-4');
    const res = await page.request.post(new URL('/api/issues/DEMO-4/comments', page.url()).href, { data: { Markdown: 'Seen from elsewhere' } });
    expect(res.ok()).toBe(true);
    await expect(panel(page)).not.toContainText('Seen from elsewhere');
    tell({ seq: 1, kind: 'comment', action: 'put', key: 'DEMO-4' });
    await expect(panel(page)).toContainText('Seen from elsewhere');
  });
});
