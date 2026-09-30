// Format a date with a Go time layout ("2006-01-02 15:04"), as ui.date_format is written.
const MON = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DAY = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const p2 = n => String(n).padStart(2, '0');
// Longest first so "January" wins over "Jan" and "2006" over "2".
const TOKENS = [
  ['January', d => MON[d.getMonth()]], ['Monday', d => DAY[d.getDay()]], ['2006', d => String(d.getFullYear())],
  ['Jan', d => MON[d.getMonth()].slice(0, 3)], ['Mon', d => DAY[d.getDay()].slice(0, 3)], ['MST', d => (d.toLocaleTimeString('en', { timeZoneName: 'short' }).split(' ').pop())],
  ['01', d => p2(d.getMonth() + 1)], ['02', d => p2(d.getDate())], ['03', d => p2(d.getHours() % 12 || 12)], ['04', d => p2(d.getMinutes())], ['05', d => p2(d.getSeconds())],
  ['06', d => p2(d.getFullYear() % 100)], ['15', d => p2(d.getHours())], ['PM', d => (d.getHours() < 12 ? 'AM' : 'PM')], ['pm', d => (d.getHours() < 12 ? 'am' : 'pm')],
  ['_2', d => String(d.getDate()).padStart(2, ' ')], ['1', d => String(d.getMonth() + 1)], ['2', d => String(d.getDate())], ['3', d => String(d.getHours() % 12 || 12)],
];
export function goDate(d, layout) {
  if (!(d instanceof Date)) d = new Date(d);
  if (isNaN(d)) return '';
  let out = '';
  for (let i = 0; i < layout.length;) {
    const t = TOKENS.find(([k]) => layout.startsWith(k, i));
    if (t) { out += t[1](d); i += t[0].length; } else out += layout[i++];
  }
  return out;
}
