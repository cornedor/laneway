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
		"status": &k.JiraStatus, "priority": &k.JiraPriority, "points": &k.JiraPoints, "summary": &k.JiraSummary, "labels": &k.JiraLabels,
		"assign": &k.JiraAssignee, "comment": &k.JiraComment, "reply": &k.JiraReply,
		"start_work": &k.JiraStart, "linked_issue": &k.JiraLinks, "back": &k.Back, "image": &k.Image,
		"quit": &k.Quit, "help": &k.Help, "search": &k.Search, "goto": &k.Goto, "create": &k.Create,
		"copy_key": &k.CopyKey, "copy_url": &k.CopyURL,
		"move_left": &k.MoveCardLeft, "move_right": &k.MoveCardRight,
		"project": &k.Project, "board": &k.Board, "next_view": &k.NextView, "prev_view": &k.PrevView,
		"toggle_mode": &k.ToggleMode, "sort": &k.Sort, "move_sprint": &k.MoveSprint,
		"assignee_filter": &k.Assignee, "mine": &k.Mine, "clear_filters": &k.ClearFilters, "roadmap": &k.Roadmap, "palette": &k.Palette, "mark": &k.Mark, "bulk": &k.Bulk, "plan": &k.Plan, "charts": &k.Charts, "log_work": &k.LogWork, "timer": &k.Timer, "timesheet": &k.Timesheet,
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
	return append(warn, k.clashes()...)
}

// keyScopes lists the actions live at once, by the ui.keys name. Within
// one scope a key must do one thing.
var keyScopes = []struct {
	name    string
	actions []string
}{
	{"board", []string{
		"up", "down", "left", "right", "top", "bottom", "page_up", "page_down",
		"open", "toggle_panel", "browser", "refresh", "quit", "help", "search", "goto", "create",
		"copy_key", "copy_url", "move_left", "move_right", "project", "board",
		"next_view", "prev_view", "toggle_mode", "sort", "move_sprint", "assignee_filter", "mine", "clear_filters", "roadmap", "palette", "mark", "bulk", "plan", "charts", "timer", "timesheet",
	}},
	{"panel", []string{
		"status", "priority", "points", "summary", "labels", "assign", "comment", "reply", "start_work",
		"linked_issue", "back", "image", "browser", "copy_key", "copy_url", "help", "refresh", "toggle_panel", "palette",
		"log_work", "timer", "timesheet",
	}},
}

// clashes reports keys bound to two actions in one scope.
func (k *keyMap) clashes() []string {
	names := k.keyNames()
	var warn []string
	for _, s := range keyScopes {
		owner := map[string]string{}
		for _, name := range s.actions {
			for _, kk := range names[name].Keys() {
				if prev, ok := owner[kk]; ok {
					warn = append(warn, fmt.Sprintf("ui.keys: %q is both %s and %s on the %s", kk, prev, name, s.name))
					continue
				}
				owner[kk] = name
			}
		}
	}
	return warn
}

// keysLabel is a binding's keys for the help overlay: "H / shift+left".
func keysLabel(b key.Binding) string {
	return strings.Join(b.Keys(), " / ")
}
