package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// listNav moves a list cursor idx over n rows by the same keys everywhere:
// up/down (ctrl+p/ctrl+n), page up/down by page rows, top/bottom. While
// typing (a filter box has the keys) only keys that type nothing move it:
// the arrows, ctrl+p/ctrl+n, pgup/pgdown. ok is false for another key.
func (k *keyMap) listNav(msg tea.KeyPressMsg, idx, n, page int, typing bool) (int, bool) {
	last := max(n-1, 0)
	s := msg.String()
	switch {
	case key.Matches(msg, k.InputUp), !typing && key.Matches(msg, k.Up):
		return max(idx-1, 0), true
	case key.Matches(msg, k.InputDown), !typing && key.Matches(msg, k.Down):
		return min(idx+1, last), true
	case s == "pgup", !typing && key.Matches(msg, k.PageUp):
		return max(idx-max(page, 1), 0), true
	case s == "pgdown", !typing && key.Matches(msg, k.PageDown):
		return min(idx+max(page, 1), last), true
	case !typing && key.Matches(msg, k.Home):
		return 0, true
	case !typing && key.Matches(msg, k.End):
		return last, true
	}
	return idx, false
}
