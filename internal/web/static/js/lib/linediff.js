// lineDiff(from, to, limit): the lines from and to differ in, "- old" and "+ new" in order
// (a longest common subsequence apart), at most limit with "… n more" after (TUI lineDiff).
export function lineDiff(from, to, limit = 12) {
  const a = from.split('\n'), b = to.split('\n');
  // lcs[i][j] is the common run of a[i:] and b[j:].
  const lcs = Array.from({ length: a.length + 1 }, () => new Int32Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--) for (let j = b.length - 1; j >= 0; j--) lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const out = [];
  let i = 0, j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) { i++; j++; }
    else if (i < a.length && (j === b.length || lcs[i + 1][j] >= lcs[i][j + 1])) out.push('- ' + a[i++]);
    else out.push('+ ' + b[j++]);
  }
  return out.length > limit ? [...out.slice(0, limit), '  … ' + (out.length - limit) + ' more'] : out;
}
