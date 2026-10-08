// Posting a comment. A post Jira answered too late (504) may have landed all
// the same: the same text posted again on that issue asks the server to look
// for it among the newest comments first, so it is not posted twice (TUI
// applyJiraComment).
import { T } from './i18n.js';

// mentionsIn is who text mentions, as an editor draws them: those picked
// ({AccountID, DisplayName}), then the issue's people ([name, accountId]) typed
// by hand or restored with a draft.
export function mentionsIn(text, picked, people) {
  const all = [...picked];
  for (const [n, id] of people) if (id && !all.some(m => m.AccountID === id)) all.push({ AccountID: id, DisplayName: n });
  return all.filter(m => text.includes('@' + m.DisplayName));
}

const maybe = new Map(); // issue key → the text that may have posted

// postComment posts body ({Markdown | Doc, …}) on key; true when it was there already.
export async function postComment(api, key, body) {
  const sig = body.Doc ? JSON.stringify(body.Doc) : body.Markdown;
  const check = maybe.get(key) === sig;
  try {
    const r = await api.post('/issues/' + key + '/comments', check ? { ...body, Check: true } : body);
    maybe.delete(key);
    return !!(r && r.Found);
  } catch (e) {
    if (e.status === 504 || check) {
      maybe.set(key, sig);
      e.message = T('The comment may have posted: %s. Posting it again looks for it first.', e.message);
    }
    throw e;
  }
}
