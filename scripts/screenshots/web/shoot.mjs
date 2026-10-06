#!/usr/bin/env node
// Screenshots of laneway web -demo for the docs, as PNGs.
//
//   node shoot.mjs <outdir> <baseURL> [name…]   (default: every shot)
//
// 1440x900 at 1x, dark theme, reduced motion. CHROMIUM, when set, is the
// browser; else Playwright's own (npx playwright install chromium).

import { chromium } from 'playwright';
import { mkdirSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';

const [outDir, baseURL, ...only] = process.argv.slice(2);
if (!baseURL) {
  console.error('usage: shoot.mjs <outdir> <baseURL> [name…]');
  process.exit(2);
}
const out = resolve(outDir);
mkdirSync(out, { recursive: true });

// Each shot: open a route, act as a user would, then wait for what it shows.
// keys() presses keys 120ms apart; a chord (g p) needs both within 2.5s.
// The board's O sorts, so My work is g w; the demo logs work yesterday, so W steps back a day.
const SHOTS = {
  board: async ({ open }) => { await open('/board', '.bd-lane .card'); },
  list: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('t'); await ready('.lrow'); },
  panel: async ({ open, ready }) => { await open('/board?issue=DEMO-4', '.bd-lane .card'); await ready('#panel:not([hidden]) .iss .desc'); },
  help: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('?'); await ready('.help section'); },
  palette: async ({ open, keys, type, ready }) => { await open('/board', '.bd-lane .card'); await keys(':'); await ready('.pal-row'); await type('go'); await ready('.pal-row'); },
  jql: async ({ page, open, keys, type, ready }) => {
    await open('/board', '.bd-lane .card'); await keys('Q'); await ready('.pal');
    await page.evaluate(() => { document.activeElement.spellcheck = false; }); // no squiggle under currentUser()
    await type('assignee = currentUser() AND status = "In '); await ready('.pal-row');
  },
  planning: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('g', 'p'); await ready('.pl .lrow .l-key'); },
  burndown: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('g', 'r'); await ready('.rp .ch-line'); },
  roadmap: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('g', 'm'); await ready('.rm-bar'); await keys('Space'); await ready('.rm-row + .rm-row .rm-bar'); },
  inbox: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('I'); await ready('.inbox-detail .ientry'); },
  standup: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('U'); await ready('.standup .strow'); await ready('.stboard .card'); },
  worklogs: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('W'); await ready('.time-head'); await keys('h'); await ready('.wlist .wlog'); },
  mywork: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('g', 'w'); await ready('.wrow, .time-head'); await keys('1'); await ready('.wrow .wkey'); },
  mr: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('g', 'M'); await ready('.mr-row'); await keys('Enter'); await ready('.mrp-pipeline'); },
  review: async ({ page, open, keys, ready }) => {
    await open('/board', '.bd-lane .card'); await keys('g', 'M'); await ready('.mr-row'); await keys('Enter'); await ready('.mrp-pipeline');
    await keys('2'); await ready('.mrp-changes .df-row'); await keys('n'); await page.waitForTimeout(300);
  },
  settings: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('g', ','); await ready('.st .st-row'); },
  // T times the selected card first, so Home's timer widget has one.
  home: async ({ open, keys, ready }) => { await open('/board', '.bd-lane .card'); await keys('j', 'T'); await ready('.timer-chip'); await keys('g', 'h'); await ready('.home .hrow'); },
};

const names = only.length ? only : Object.keys(SHOTS);
const unknown = names.filter(n => !SHOTS[n]);
if (unknown.length) {
  console.error('unknown shot: ' + unknown.join(', ') + '\nshots: ' + Object.keys(SHOTS).join(' '));
  process.exit(2);
}

const browser = await chromium.launch({ executablePath: process.env.CHROMIUM || undefined });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, baseURL, colorScheme: 'dark', reducedMotion: 'reduce' });
await context.addInitScript(() => {
  try { localStorage.setItem('lw:theme', 'dark'); localStorage.setItem('lw:motion', 'reduce'); } catch (e) { /* private mode */ }
});

let failed = 0;
for (const name of names) {
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const ready = sel => page.locator(sel).filter({ visible: true }).first().waitFor({ timeout: 15000 });
  const ctx = {
    page, ready,
    open: async (route, sel) => { await page.goto('/#' + route); await ready(sel); },
    keys: async (...ks) => { for (const k of ks) { await page.keyboard.press(k); await page.waitForTimeout(120); } },
    type: t => page.keyboard.type(t, { delay: 20 }),
  };
  try {
    await SHOTS[name](ctx);
    const toasts = await page.locator('.toast.err, .kb-msg.err').allInnerTexts();
    if (toasts.length) throw new Error('error toast: ' + toasts.join(' | '));
    // Settled: no loading rows or messages, fonts in, two frames painted.
    await page.waitForFunction(() => ![...document.querySelectorAll('.loading, .df-msg, .toast, .kb-msg')].some(e => e.offsetParent), null, { timeout: 15000 });
    await page.evaluate(() => document.fonts.ready.then(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))));
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${out}/${name}.png` });
    rmSync(`${out}/${name}-failure.png`, { force: true });
    console.log(`shot: ${name}`);
  } catch (e) {
    failed++;
    console.error(`${name} failed: ${e.message.split('\n')[0]}${errors.length ? ' (page: ' + errors.join('; ') + ')' : ''}`);
    await page.screenshot({ path: `${out}/${name}-failure.png` }).catch(() => {});
  }
  await page.close();
}
await browser.close();
process.exit(failed ? 1 : 0);
