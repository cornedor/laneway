import { appendFileSync, mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

// INP at most 100ms, where a response stops feeling instant (the web's
// "good" is 200ms; this is an app); CLS at most 0.1, the web's "good".
const good = { INP: 100, CLS: 0.1 };
const show = { INP: v => `${Math.round(v)}ms`, CLS: v => v.toFixed(3) };

// VitalsReporter sums up the vitals attachments (fixtures.mjs): each test's
// worst INP and CLS, the run's worst first. It writes them to
// test-results/vitals.json and, on GitHub, to the job summary.
export default class VitalsReporter {
  constructor() { this.tests = new Map(); }

  onTestEnd(test, result) {
    const a = result.attachments.find(a => a.name === 'vitals');
    if (a) this.tests.set(test.id, { title: test.titlePath().slice(2).join(' › '), ...JSON.parse(a.body.toString()) });
  }

  onEnd() {
    const tests = [...this.tests.values()];
    if (!tests.length) return;
    const lines = [], md = [process.env.INP_BUDGETS ? '### Web vitals, INP budgets (serial)' : '### Web vitals', ''];
    for (const name of Object.keys(good)) {
      const worst = tests.filter(t => t[name]).sort((a, b) => b[name].value - a[name].value);
      const over = worst.filter(t => t[name].value > good[name]);
      lines.push(`${name}: ${over.length} of ${tests.length} tests over ${show[name](good[name])}`);
      md.push(`**${name}**: ${over.length} of ${tests.length} tests over ${show[name](good[name])}`, '',
        '| | test | target |', '|--:|---|---|');
      for (const t of worst.slice(0, 5)) {
        // INP's phases: input delay / processing / presentation
        const m = t[name], where = [m.phases && m.phases.join('/'), m.type, m.target].filter(Boolean).join(' ');
        lines.push(`  ${show[name](m.value).padStart(7)}  ${t.title}${where ? `  (${where})` : ''}`);
        md.push(`| ${show[name](m.value)} | ${t.title} | \`${where || m.url}\` |`);
      }
      md.push('');
    }
    console.log('\n' + lines.join('\n'));
    mkdirSync('test-results', { recursive: true });
    writeFileSync(path.join('test-results', 'vitals.json'), JSON.stringify(tests, null, 2));
    if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, md.join('\n') + '\n');
  }

  printsToStdio() { return false; }
}
