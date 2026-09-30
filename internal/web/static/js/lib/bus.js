// Event bus. Standard events:
//   issue:changed {key}     after any write to an issue; the board and panel refetch
//   issue:open {key}        a request to show an issue in the panel
//   route {name, params}    the route changed
const h = new Map();
export const bus = {
  on(ev, fn) { (h.get(ev) || h.set(ev, new Set()).get(ev)).add(fn); return () => h.get(ev).delete(fn); },
  emit(ev, data) { for (const fn of [...(h.get(ev) || [])]) { try { fn(data); } catch (e) { console.error(e); } } },
};
export default bus;
