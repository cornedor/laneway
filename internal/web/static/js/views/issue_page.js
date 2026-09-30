// Route /issue/:key: the issue panel as a page.
import { mountIssue } from './issue.js';

export default function mount(el, { app, params }) {
  return mountIssue(el, params.key, { app, full: true });
}
