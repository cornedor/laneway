#!/usr/bin/env node
// Record a scenario against laneway web into an H.264 mp4 (15 fps, no audio, faststart).
//
//   node record.mjs <scenario.mjs> <out.mp4> <baseURL>
//
// A scenario is an ES module:
//   export default async ({ page, step, pause }) => { ... }
// step('Text') shows a caption at the bottom of the video; pause(ms) holds the frame.
// CHROMIUM, when set, is the browser; else Playwright's own (npx playwright install chromium).

import { chromium } from 'playwright';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const [scenarioPath, outPath, baseURL] = process.argv.slice(2);
if (!baseURL) {
  console.error('usage: record.mjs <scenario.mjs> <out.mp4> <baseURL>');
  process.exit(2);
}
const out = resolve(outPath);
const width = 1280, height = 720;
const tmp = mkdtempSync(join(process.env.TMPDIR || tmpdir(), 'laneway-video-'));
const scenario = (await import(pathToFileURL(resolve(scenarioPath)).href)).default;

const browser = await chromium.launch({ executablePath: process.env.CHROMIUM || undefined });
const context = await browser.newContext({
  viewport: { width, height },
  baseURL,
  recordVideo: { dir: tmp, size: { width, height } },
});
const page = await context.newPage();
const t0 = Date.now();

async function step(text) {
  await page.evaluate((t) => {
    let el = document.getElementById('__video_caption');
    if (!el) {
      el = document.createElement('div');
      el.id = '__video_caption';
      el.style.cssText =
        'position:fixed;left:50%;bottom:24px;transform:translateX(-50%);z-index:2147483647;' +
        'background:rgba(0,0,0,.8);color:#fff;font:600 18px/1.3 system-ui,sans-serif;' +
        'padding:8px 16px;border-radius:6px;pointer-events:none;max-width:90vw;text-align:center';
      document.documentElement.appendChild(el);
    }
    el.textContent = t;
  }, text).catch(() => {});
  console.log(`step: ${text}`);
}
const pause = (ms = 1000) => page.waitForTimeout(ms);

// Skip the blank frames before the first page.
await page.goto('/', { waitUntil: 'load' });
const trim = (Date.now() - t0) / 1000;

let failed;
try {
  await scenario({ page, step, pause });
  await pause(1500); // let the last state register with the viewer
} catch (e) {
  failed = e;
  await page.screenshot({ path: out.replace(/\.mp4$/, '') + '-failure.png' }).catch(() => {});
}
const video = page.video();
await context.close();
await browser.close();
const raw = await video.path();

if (failed) {
  console.error(`scenario failed: ${failed.message}`);
  console.error(`raw recording kept at ${raw}`);
  process.exit(1);
}

execFileSync('ffmpeg', [
  '-y', '-loglevel', 'error',
  '-ss', trim.toFixed(2), '-i', raw,
  '-vf', `fps=15,scale=${width}:-2:flags=lanczos`,
  '-c:v', 'libx264', '-preset', 'slow', '-crf', '28', '-tune', 'stillimage',
  '-pix_fmt', 'yuv420p', '-movflags', '+faststart', '-an',
  out,
]);
rmSync(tmp, { recursive: true, force: true });
console.log(`video: ${out}`);
