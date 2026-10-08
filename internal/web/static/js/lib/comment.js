// Posting a comment. A post Jira answered too late (504) may have landed all
// the same: the same text posted again on that issue asks the server to look
// for it among the newest comments first, so it is not posted twice (TUI
// applyJiraComment).
import { T } from './i18n.js';

const maybe = new Map(); // issue key → the text that may have posted

// postComment posts body ({Markdown, …}) on key; true when it was there already.
export async function postComment(api, key, body) {
  const check = maybe.get(key) === body.Markdown;
  try {
    const r = await api.post('/issues/' + key + '/comments', check ? { ...body, Check: true } : body);
    maybe.delete(key);
    return !!(r && r.Found);
  } catch (e) {
    if (e.status === 504 || check) {
      maybe.set(key, body.Markdown);
      e.message = T('The comment may have posted: %s. Posting it again looks for it first.', e.message);
    }
    throw e;
  }
}
