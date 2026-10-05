import { test as base, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

// start runs `laneway web -demo` on a free port, in a home of its own, and
// resolves to the URL it prints.
function start(unhandled) {
  const home = mkdtempSync(path.join(tmpdir(), 'laneway-e2e-'));
  const proc = spawn(process.env.LANEWAY, ['web', '-demo', '-no-open', '-addr', '127.0.0.1:0'], {
    env: { ...process.env, HOME: home, XDG_CONFIG_HOME: path.join(home, '.config'), XDG_STATE_HOME: path.join(home, '.state'),
      XDG_CACHE_HOME: path.join(home, '.cache'), LANEWAY_DEMO_UNHANDLED: unhandled },
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  let log = '';
  const url = new Promise((resolve, reject) => {
    proc.stderr.on('data', d => {
      log += d;
      const m = /laneway web on (\S+)/.exec(log);
      if (m) resolve(m[1]);
    });
    proc.on('exit', code => reject(new Error(`laneway web exited ${code}:\n${log}`)));
    setTimeout(() => reject(new Error(`laneway web didn't start:\n${log}`)), 15000);
  });
  const stop = () => { proc.kill(); rmSync(home, { recursive: true, force: true }); };
  return { url, stop, log: () => log };
}

// test is Playwright's with app: a fresh demo, the page on its board. A
// test fails on an error in the page or a request the demo can't answer.
export const test = base.extend({
  app: async ({ page }, use, info) => {
    const unhandled = info.outputPath('unhandled.txt');
    const server = start(unhandled);
    const errors = [];
    page.on('pageerror', e => errors.push(`pageerror: ${e.message}`));
    // A failed request is reported with its URL below, not as the console's
    // "Failed to load resource".
    page.on('console', m => {
      if (m.type() === 'error' && !m.text().startsWith('Failed to load resource')) errors.push(`console: ${m.text()}`);
    });
    page.on('response', r => { if (r.status() >= 400) errors.push(`${r.status()} ${r.request().method()} ${r.url()}`); });
    try {
      const url = await server.url;
      await use({ url, errors });
    } finally {
      server.stop();
    }
    let missed = '';
    try { missed = readFileSync(unhandled, 'utf8'); } catch {}
    expect(missed, 'requests the demo could not answer').toBe('');
    expect(errors, 'errors in the page').toEqual([]);
  },
});

// palette opens the palette with key, types text once its input has the
// focus, and waits for an option holding pick (text itself without one).
export async function palette(page, key, text, pick = text) {
  await page.keyboard.press(key);
  const input = page.getByRole('textbox', { name: 'Palette' });
  await expect(input).toBeFocused();
  await input.pressSequentially(text);
  await expect(page.getByRole('option', { name: pick }).first()).toBeVisible();
  return page.getByRole('option', { name: pick }).first();
}

export { expect };
