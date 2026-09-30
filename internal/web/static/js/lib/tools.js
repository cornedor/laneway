// Server-side tools the TUI has: rules feed, your ui.actions, ask the LLM, the offline write queue.
export function install(app) {
  for (const n of ['rules_feed', 'useractions', 'ask', 'offline']) {
    import('./' + n + '.js').then(m => m.install(app)).catch(e => console.error(n, e));
  }
}
