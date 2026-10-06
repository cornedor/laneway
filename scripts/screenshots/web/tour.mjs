// A tour of laneway web -demo, board to GitLab review: ./video.sh records it.
export default async ({ page, step, pause }) => {
  // k presses keys gap ms apart; a chord (g p) needs its keys within 2.5s (CHORD_MS in lib/keys.js).
  const k = async (keys, gap = 350) => { for (const key of keys) { await page.keyboard.press(key); await pause(gap); } };
  const type = t => page.keyboard.type(t, { delay: 70 });

  await page.goto('/');
  await page.getByText('DEMO-5').first().waitFor();
  await step('laneway web: your Jira sprint board, keyboard first');
  await pause(2000);
  await k(['j', 'l', 'l', 'j']);
  await pause(600);
  await step('enter opens the issue beside the board');
  await k(['Enter'], 2500);
  await step('tabs: comments, history, development');
  await k(['2'], 1800);
  await k(['3'], 1800);
  await k(['1'], 800);
  await k(['Escape'], 800);

  await step('L moves the card a column right, into Done');
  await k(['L'], 2800);

  await step(': the palette reaches every command');
  await k([':'], 600);
  await type('roadmap');
  await pause(1500);
  await k(['Escape'], 500);

  await step('g p planning: sprints and backlog');
  await k(['g', 'p'], 500); await pause(2200);
  await step('g r reports');
  await k(['g', 'r'], 500); await pause(2200);
  await step('g m roadmap');
  await k(['g', 'm'], 500); await pause(2200);

  await step('GitLab: g M lists the merge requests waiting on you');
  await k(['g', 'M'], 500); await pause(2200);
  await k(['Enter'], 600);
  await page.getByText('PIPELINE', { exact: false }).first().waitFor();
  await step('The merge request: pipeline, approvals, the Jira issue it names');
  await pause(2500);
  await step('A job shows its log, followed while it runs');
  await page.getByText('load-test', { exact: true }).first().click();
  await pause(3500);
  await k(['Escape'], 600);

  await step('2: the changes, highlighted, threads under their lines');
  await k(['2'], 2500);
  await step('A click on a line number: a note into your pending review');
  const row = page.locator('.df-row.can', { hasText: 'until time.Time' }).first();
  await row.locator('.df-no').nth(1).click();
  await pause(600);
  await type('Does the cut-off follow the carrier\'s time zone?');
  await pause(800);
  await page.keyboard.press('Control+Enter');
  await page.getByText('Submit review · 1 pending note').waitFor();
  await pause(2500);
  await step('S submits it: comment, approve or request changes');
  await k(['S'], 3000);
  await k(['Escape'], 800);
};
