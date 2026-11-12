// The All issues page: the board's all issues view as a place of its own (/issues/PROJECT/BOARD; the TUI's
// jiraViewAll), on the remembered project and board when none is named.
import mount from './board.js';

export default (el, ctx) => mount(el, { ...ctx, issues: true });
