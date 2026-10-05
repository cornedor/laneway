import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));

// Builds the laneway under test into .run, unless LANEWAY names one.
export default function build() {
  if (process.env.LANEWAY) return;
  const out = path.join(here, '.run', 'laneway');
  execFileSync('go', ['build', '-o', out, '.'], { cwd: path.join(here, '..', '..'), stdio: 'inherit' });
  process.env.LANEWAY = out;
}
