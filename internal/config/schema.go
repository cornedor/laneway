package config

// What the settings screens (terminal and web) say about each ui: option.

// SettingsRestart are options read once at startup.
var SettingsRestart = map[string]bool{"images": true, "threaded_replies": true, "image_max_rows": true, "card_limit": true, "default_mode": true, "flag_value": true, "inbox_issues": true, "custom_fields": true}

// SettingDefaults is each ui: option's default as the README shows it.
var SettingDefaults = map[string]string{
	"auto_refresh":         "2m",
	"stale_after":          "1m",
	"images":               "auto",
	"image_max_rows":       "16",
	"card_limit":           "500",
	"panel_width":          "50",
	"keys":                 "as in ?",
	"default_mode":         "lanes",
	"date_format":          "2006-01-02 15:04",
	"card_fields":          "all",
	"home":                 "none",
	"calendar":             "none",
	"meeting_key":          "none",
	"quick_filters":        "none",
	"views":                "none",
	"stale_days":           "5",
	"velocity_sprints":     "8",
	"templates":            "none",
	"timer_on_start":       "off",
	"start_assigns":        "off",
	"start_status":         "none",
	"workday_start":        "09:00",
	"capacity":             "none",
	"saved_filters":        "on",
	"delight":              "on",
	"threaded_replies":     "on",
	"skin_tone":            "none",
	"update_check":         "on",
	"llm":                  "claude -p, when claude is on the PATH",
	"branch_template":      "{key}-{summary}",
	"work_branch_template": "branch_template if set, else issue/{key}-{summary}",
	"kanban_done_days":     "14",
	"roadmap_epic_type":    "Epic",
	"my_work_jql":          "yours everywhere, open or done this week",
	"download_dir":         "$XDG_DOWNLOAD_DIR, else ~/Downloads",
	"roadmap_done_days":    "90",
	"workdays":             "mon–fri",
	"inbox_every":          "5m",
	"inbox_lookback":       "168h",
	"inbox_issues":         "30",
	"timer_round":          "to the minute",
	"clipboard_image":      "wl-paste, xclip or pngpaste",
	"open":                 "xdg-open, open or rundll32",
	"full_refresh":         "10m",
	"filters":              "none",
	"card_colors":          "ribbon",
	"mouse":                "on",
	"double_click":         "400ms",
	"icons":                "nerd",
	"empty_fields":         "show",
	"empty_lanes":          "show",
	"custom_fields":        "none",
	"flag_value":           "Impediment",
	"work_agent":           "claude",
	"agent_view":           "fullscreen",
	"work_args":            "none",
	"work_create":          "herdr creates the worktree",
	"code_theme":           "the preset's, else monokai",
	"theme":                "terminal colours",
	"actions":              "none",
	"activity":             "none: git in jira.repos only",
}

// SettingGroup is a heading of the settings screen and its options.
type SettingGroup struct {
	Title string
	Names []string
}

// SettingGroups are the settings screen's headings and their options, in
// order; an option in none shows under Other.
var SettingGroups = []SettingGroup{
	{"Board and cards", []string{"default_mode", "card_fields", "card_colors", "empty_lanes", "custom_fields", "card_limit", "kanban_done_days", "stale_days", "flag_value", "icons"}},
	{"Views and filters", []string{"home", "quick_filters", "views", "saved_filters", "filters", "my_work_jql"}},
	{"Panel", []string{"panel_width", "empty_fields", "date_format", "images", "image_max_rows", "templates", "code_theme", "threaded_replies"}},
	{"Refresh", []string{"auto_refresh", "stale_after", "full_refresh"}},
	{"Time and worklogs", []string{"timer_round", "timer_on_start", "workday_start", "workdays", "capacity", "activity", "calendar", "meeting_key"}},
	{"Start work and agents", []string{"start_assigns", "start_status", "branch_template", "work_branch_template", "work_agent", "work_args", "work_create", "agent_view", "llm", "actions"}},
	{"Planning, roadmap and charts", []string{"velocity_sprints", "roadmap_epic_type", "roadmap_done_days"}},
	{"Inbox", []string{"inbox_every", "inbox_lookback", "inbox_issues"}},
	{"Look and feel", []string{"theme", "mouse", "double_click", "keys", "delight", "skin_tone"}},
	{"System", []string{"open", "clipboard_image", "download_dir", "update_check"}},
}

// SettingDocs is a line on each ui: option, as its config doc says.
var SettingDocs = map[string]string{
	"auto_refresh":         "how often an idle board refetches; off stops it",
	"stale_after":          "how old a board may be before focus or a tick refetches it",
	"images":               "inline images in the panel (kitty, Ghostty): auto or off",
	"image_max_rows":       "the tallest an inline image gets, in rows",
	"card_limit":           "the most cards one view fetches",
	"panel_width":          "the issue panel's share of the width, in percent",
	"keys":                 "rebinds actions by name: search: \"/\" or mine: [m, M]",
	"default_mode":         "the board's mode before one is remembered",
	"date_format":          "a Go time layout for the panel's dates",
	"calendar":             "an iCalendar feed of your meetings (file, https or webcal URL, or a vdir directory): the timesheet proposes them as worklogs, planning takes them off your capacity",
	"meeting_key":          "the issue meetings from ui.calendar are logged on",
	"home":                 "the start screen's widgets, in order: work, inbox, sprint, timer, reviews, filters",
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
	"inbox_every":          "how often the inbox syncs, for the header's count; off stops it",
	"inbox_lookback":       "how far back the inbox reaches",
	"inbox_issues":         "how many recently updated issues the inbox and standup read",
	"timer_round":          "rounds the timer's logged time up to a step (15m)",
	"clipboard_image":      "a command printing the clipboard's PNG",
	"open":                 "a command that opens URLs and files",
	"full_refresh":         "how long idle refreshes fetch only changes before a whole refetch (terminal only: the browser always refetches the board)",
	"filters":              "named / queries to recall from the : palette",
	"card_colors":          "how cards show the board's card colours",
	"mouse":                "clicks, drags and the wheel; off leaves the mouse to the terminal",
	"llm":                  "a command answering ctrl+a's questions about the issue, piped on stdin",
	"delight":              "small celebrations: confetti on a card into done, a line on a completed sprint",
	"skin_tone":            "the tone : completion offers for people and hands",
	"threaded_replies":     "a reply goes under its comment in Jira's thread (and shows there); off: replies are new comments, quoted",
	"update_check":         "once a day, whether a newer release exists; never on a dev build",
	"double_click":         "how quickly a second click makes a double-click (100ms–2s)",
	"icons":                "issue type icons: Nerd Font glyphs, or letters for fonts without them",
	"empty_fields":         "every editable field in the panel, or empty ones folded behind a row",
	"empty_lanes":          "board columns with no card after the filters: shown, or hidden for the rest's room; the board's toggle is remembered over it",
	"custom_fields":        "Jira fields by name that cards show and / searches",
	"flag_value":           "the Flagged field's option flagging sets",
	"work_agent":           "the herdr agent kind S launches",
	"agent_view":           "where an attached agent shows: fullscreen or in the panel",
	"work_args":            "the agent's arguments before the start prompt, {key} replaced",
	"work_create":          "a command that makes a missing worktree: {branch}, {base}, {key}",
	"code_theme":           "the chroma style code blocks use (terminal only: the browser's follow its theme)",
	"theme":                "a preset name, or colours by name over an optional preset",
	"actions":              "your own commands on the issue: in the palette, on a key if given",
	"activity":             "commands printing \"time<TAB>key\" lines of a day's work, for the timesheet's proposals",
}

// SettingChoices are the values an option with a fixed set takes, the
// default first; nil for the others. (code_theme and theme are the UI's.)
func SettingChoices(name string) []string {
	switch name {
	case "images":
		return []string{"auto", "off"}
	case "default_mode":
		return []string{"lanes", "list"}
	case "card_colors":
		return []string{"ribbon", "off"}
	case "agent_view":
		return []string{"fullscreen", "panel"}
	case "icons":
		return []string{"nerd", "plain"}
	case "empty_fields", "empty_lanes":
		return []string{"show", "hide"}
	case "saved_filters", "mouse", "delight", "update_check", "threaded_replies":
		return []string{"on", "off"}
	case "skin_tone":
		return []string{"none", "light", "medium_light", "medium", "medium_dark", "dark"}
	case "timer_on_start", "start_assigns":
		return []string{"off", "on"}
	}
	return nil
}
