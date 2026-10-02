// Route table. Each view module default-exports mount(el, ctx) → cleanup?
// bar: false marks a view that never fills the view bar (it hides at once instead of holding its place).
// ctx: {app, params, query, scope (a keys scope disposed on leave), context + toolbar (the view bar: where, then how)}.
// Hash routes: #/board/PROJECT/BOARDID?issue=KEY … (':x' are params).
// A missing module shows a "not built yet" page; nothing else breaks.
export const routes = [
  { path: '/home', name: 'home', title: 'Home', key: 'h', bar: false, load: () => import('./home.js') },
  { path: '/board/:project?/:board?', name: 'board', title: 'Board', key: 'b', load: () => import('./board.js') },
  { path: '/issue/:key', name: 'issue', title: 'Issue', nav: false, bar: false, load: () => import('./issue_page.js') },
  { path: '/planning/:project?/:board?', name: 'planning', title: 'Planning', key: 'p', load: () => import('./planning.js') },
  { path: '/reports/:kind?/:project?/:board?', name: 'reports', title: 'Reports', key: 'r', load: () => import('./reports.js') },
  { path: '/roadmap/:project?', name: 'roadmap', title: 'Roadmap', key: 'm', load: () => import('./roadmap.js') },
  { path: '/work', name: 'work', title: 'My work', key: 'w', load: () => import('./work.js') },
  { path: '/inbox', name: 'inbox', title: 'Inbox', key: 'i', load: () => import('./inbox.js') },
  { path: '/standup', name: 'standup', title: 'Standup', key: 's', load: () => import('./standup.js') },
  { path: '/agents', name: 'agents', title: 'Agents', key: 'a', bar: false, load: () => import('./agents.js') },
  { path: '/review', name: 'review', title: 'Review', key: 'R', bar: false, load: () => import('./review.js') },
  { path: '/mrs', name: 'mrs', title: 'Merge requests', key: 'M', bar: false, load: () => import('./mrs.js') },
  { path: '/rules', name: 'rules', title: 'Rules', key: 'l', nav: false, load: () => import('./rules.js') },
  { path: '/settings', name: 'settings', title: 'Settings', key: ',', nav: false, load: () => import('./settings.js') },
];
