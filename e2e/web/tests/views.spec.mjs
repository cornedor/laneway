import { test, expect } from '../fixtures.mjs';

// Every view draws from the demo with no error in the page and no request
// the demo can't answer (fixtures.mjs checks both).
const views = {
  '/home': 'My work',
  '/planning': 'Backlog',
  '/reports': 'Velocity',
  '/roadmap': 'Guest checkout',
  '/work': 'DEMO-',
  '/inbox': 'DEMO-',
  '/standup': 'Jamie Rivers',
  '/mrs': '!',
  '/rules': 'Rules',
  '/settings': 'Appearance',
  '/issue/DEMO-4': 'Checkout without an account',
};

for (const [route, text] of Object.entries(views)) {
  test(`${route} draws`, async ({ page, app }) => {
    await page.goto(new URL('#' + route, app.url).href);
    await expect(page.locator('main, #app, body').first()).toContainText(text);
  });
}
