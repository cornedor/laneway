package ui

import (
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2/styles"
	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/config"
)

// What the settings overlay says about an option: a line on what it does,
// and for one with a fixed set of values, those to pick from.

// settingDocs is a line on each ui: option, as its config doc says.
var settingDocs = map[string]string{
	"auto_refresh":         "how often an idle board refetches; off stops it",
	"stale_after":          "how old a board may be before focus or a tick refetches it",
	"images":               "inline images in the panel (kitty, Ghostty): auto or off",
	"image_max_rows":       "the tallest an inline image gets, in rows",
	"card_limit":           "the most cards one view fetches",
	"panel_width":          "the issue panel's share of the width, in percent",
	"keys":                 "rebinds actions by name: search: \"/\" or mine: [m, M]",
	"default_mode":         "the board's mode before one is remembered",
	"date_format":          "a Go time layout for the panel's dates",
	"card_fields":          "what cards show, in order: type, priority, status, points, assignee, parent",
	"quick_filters":        "JQL presets shown before every board's own",
	"views":                "JQL-narrowed views of every board, after its own",
	"stale_days":           "days in progress before a card's age shows red",
	"velocity_sprints":     "how many closed sprints the velocity chart shows",
	"templates":            "the description a new issue starts with, by type (markdown)",
	"timer_on_start":       "start the timer when S starts work on an issue",
	"start_assigns":        "assign the issue to you when S starts work on it",
	"start_status":         "the status S moves the issue to (In Progress); empty for none",
	"workday_start":        "when work logged on another day starts",
	"capacity":             "story points per person a sprint holds; default for everyone else",
	"saved_filters":        "your starred Jira filters as views",
	"branch_template":      "the branch ctrl+y copies: {key} {summary} {type} {project}",
	"work_branch_template": "the branch S creates, same placeholders",
	"kanban_done_days":     "how many days done work stays on a kanban board",
	"roadmap_epic_type":    "the issue type the roadmap shows and creates",
	"my_work_jql":          "the query O's my work view runs",
	"download_dir":         "where attachments are saved",
	"roadmap_done_days":    "how long resolved epics stay on the roadmap",
	"workdays":             "the days you work, for the standup's previous workday and the week's gaps",
	"inbox_every":          "how often the header's inbox count refreshes; off stops it",
	"inbox_lookback":       "how far back a first inbox read looks",
	"inbox_issues":         "how many recently updated issues the inbox and standup read",
	"timer_round":          "rounds the timer's logged time up to a step (15m)",
	"clipboard_image":      "a command printing the clipboard's PNG",
	"open":                 "a command that opens URLs and files",
	"full_refresh":         "how long idle refreshes fetch only changes before a whole refetch",
	"filters":              "named / queries to recall from the : palette",
	"card_colors":          "how cards show the board's card colours",
	"mouse":                "clicks, drags and the wheel; off leaves the mouse to the terminal",
	"llm":                  "a command answering ctrl+a's questions about the issue, piped on stdin",
	"delight":              "small celebrations: confetti on a card into done, a line on a completed sprint",
	"double_click":         "how quickly a second click makes a double-click (100ms–2s)",
	"icons":                "issue type icons: Nerd Font glyphs, or letters for fonts without them",
	"empty_fields":         "every editable field in the panel, or empty ones folded behind a row",
	"custom_fields":        "Jira fields by name that cards show and / searches",
	"flag_value":           "the Flagged field's option flagging sets",
	"work_agent":           "the herdr agent kind S launches",
	"code_theme":           "the chroma style code blocks use",
	"theme":                "a preset name, or colours by name over an optional preset",
	"actions":              "your own commands on the issue: in the palette, on a key if given",
}

// settingChoices are the values an option with a fixed set takes, the
// default first.
func settingChoices(name string) []string {
	switch name {
	case "images":
		return []string{"auto", "off"}
	case "default_mode":
		return []string{"lanes", "list"}
	case "card_colors":
		return []string{"ribbon", "off"}
	case "icons":
		return []string{"nerd", "plain"}
	case "empty_fields":
		return []string{"show", "hide"}
	case "saved_filters", "mouse", "delight":
		return []string{"on", "off"}
	case "timer_on_start", "start_assigns":
		return []string{"off", "on"}
	case "code_theme":
		return styles.Names()
	case "theme":
		names := make([]string, 0, len(themePresets))
		for n := range themePresets {
			names = append(names, n)
		}
		slices.Sort(names)
		return names
	}
	return nil
}

// settingFull is an option's whole value as YAML, for one that doesn't
// fit a line; "" when it is unset or fits.
func settingFull(c config.UIConfig, name string) string {
	f := settingField(&c, name)
	if !f.IsValid() || editable(f) || f.IsZero() {
		return ""
	}
	b, err := yaml.Marshal(f.Interface())
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}
