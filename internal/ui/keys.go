package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"

	"github.com/cornedor/laneway/internal/config"
)

// keyNames names every rebindable action for the ui.keys config.
func (k *keyMap) keyNames() map[string]*key.Binding {
	return map[string]*key.Binding{
		"up": &k.Up, "down": &k.Down, "left": &k.Left, "right": &k.Right,
		"top": &k.Home, "bottom": &k.End, "page_up": &k.PageUp, "page_down": &k.PageDown,
		"open": &k.OpenChannel, "toggle_panel": &k.OpenRef, "browser": &k.OpenAttach, "refresh": &k.Refresh,
		"status": &k.JiraStatus, "priority": &k.JiraPriority, "points": &k.JiraPoints,
		"assign": &k.JiraAssignee, "comment": &k.JiraComment, "reply": &k.JiraReply,
		"start_work": &k.JiraStart, "linked_issue": &k.JiraLinks, "back": &k.Back,
		"quit": &k.Quit, "help": &k.Help, "search": &k.Search, "goto": &k.Goto,
		"copy_key": &k.CopyKey, "copy_url": &k.CopyURL,
		"move_left": &k.MoveCardLeft, "move_right": &k.MoveCardRight,
		"project": &k.Project, "board": &k.Board, "next_view": &k.NextView, "prev_view": &k.PrevView,
		"toggle_mode": &k.ToggleMode, "sort": &k.Sort,
		"assignee_filter": &k.Assignee, "mine": &k.Mine, "clear_filters": &k.ClearFilters,
	}
}

// applyKeys rebinds the named actions, keeping each one's help text.
// Unknown names and empty lists are reported and skipped.
func (k *keyMap) applyKeys(over map[string]config.KeyList) []string {
	names := k.keyNames()
	var warn []string
	for _, name := range slices.Sorted(maps.Keys(over)) {
		keys := over[name]
		b, ok := names[name]
		switch {
		case !ok:
			warn = append(warn, fmt.Sprintf("ui.keys: unknown action %q", name))
			continue
		case len(keys) == 0 || slices.Contains(keys, ""):
			warn = append(warn, fmt.Sprintf("ui.keys.%s: no key", name))
			continue
		}
		*b = key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], b.Help().Desc))
	}
	return warn
}

// keysLabel is a binding's keys for the help overlay: "H / shift+left".
func keysLabel(b key.Binding) string {
	return strings.Join(b.Keys(), " / ")
}
