// Route table. Each view module default-exports mount(el, ctx) → cleanup?
// ctx: {app, params, query, scope (a keys scope disposed on leave), toolbar (header slot)}.
// Hash routes: #/board/PROJECT/BOARDID?issue=KEY … (':x' are params).
// A missing module shows a "not built yet" page; nothing else breaks.
export const routes = [
  { path: '/board/:project?/:board?', name: 'board', title: 'Board', key: 'b', load: () => import('./board.js') },
  { path: '/issue/:key', name: 'issue', title: 'Issue', nav: false, load: () => import('./issue_page.js') },
  { path: '/planning/:project?/:board?', name: 'planning', title: 'Planning', key: 'p', load: () => import('./planning.js') },
  { path: '/reports/:kind?/:project?/:board?', name: 'reports', title: 'Reports', key: 'r', load: () => import('./reports.js') },
  { path: '/roadmap/:project?', name: 'roadmap', title: 'Roadmap', key: 'm', load: () => import('./roadmap.js') },
  { path: '/work', name: 'work', title: 'My work', key: 'w', load: () => import('./work.js') },
  { path: '/inbox', name: 'inbox', title: 'Inbox', key: 'i', load: () => import('./inbox.js') },
  { path: '/standup', name: 'standup', title: 'Standup', key: 's', load: () => import('./standup.js') },
  { path: '/settings', name: 'settings', title: 'Settings', key: ',', nav: false, load: () => import('./settings.js') },
];
