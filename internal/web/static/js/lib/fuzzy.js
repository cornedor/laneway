// Subsequence fuzzy match. Returns null (no match) or {score, idx:[matched positions]}; higher score is better.
export function fuzzy(q, s) {
  if (!q) return { score: 0, idx: [] };
  q = q.toLowerCase(); const l = s.toLowerCase();
  let qi = 0, score = 0, last = -2; const idx = [];
  for (let i = 0; i < l.length && qi < q.length; i++) {
    if (l[i] !== q[qi]) continue;
    idx.push(i);
    score += 1 + (i === last + 1 ? 3 : 0) + (i === 0 || /[\s\-_/.:]/.test(l[i - 1]) ? 4 : 0);
    last = i; qi++;
  }
  if (qi < q.length) return null;
  return { score: score - l.length * 0.01, idx };
}
export function rank(items, q, text = x => x) {
  if (!q) return items.slice();
  const out = [];
  for (const it of items) { const m = fuzzy(q, text(it)); if (m) out.push([m.score, it]); }
  return out.sort((a, b) => b[0] - a[0]).map(x => x[1]);
}
