package config

import "github.com/cornedor/laneway/internal/i18n"

// What the settings screens (terminal and web) say about each ui: option.

// SettingsRestart are options read once at startup.
var SettingsRestart = map[string]bool{"images": true, "threaded_replies": true, "image_max_rows": true, "card_limit": true, "default_mode": true, "flag_value": true, "inbox_issues": true, "custom_fields": true, "language": true}

// SettingsTerminal are options only the terminal reads; the browser has
// its own for some (theme, under Appearance).
var SettingsTerminal = map[string]bool{"theme": true, "icons": true, "mouse": true, "double_click": true, "images": true, "image_max_rows": true, "code_theme": true, "full_refresh": true}

// SettingsCommand are options laneway runs as commands (or passes to one).
var SettingsCommand = map[string]bool{"actions": true, "llm": true, "activity": true, "open": true, "clipboard_image": true, "work_agent": true, "work_args": true, "work_create": true}

// SettingsBasic are the options for making laneway your own: looks, layouts,
// views. The rest are specifics and tuning, which the browser's settings
// show behind Advanced, with SettingsTerminal.
var SettingsBasic = map[string]bool{"default_mode": true, "card_fields": true, "list_columns": true, "card_colors": true, "empty_lanes": true, "card_layout": true, "card_styles": true, "lane_layouts": true, "home": true, "quick_filters": true, "views": true, "saved_filters": true, "filters": true, "empty_fields": true, "templates": true, "comment_order": true, "comment_layout": true, "agent_view": true, "delight": true, "skin_tone": true, "language": true}

// SettingDefaults is each ui: option's default as the docs show it.
var SettingDefaults = map[string]string{
	"auto_refresh":         "2m",
	"stale_after":          "1m",
	"images":               "auto",
	"image_max_rows":       "16",
	"card_limit":           "500",
	"panel_width":          "50",
	"keys":                 "as in ?",
	"web_keys":             "as in ?",
	"default_mode":         "lanes",
	"date_format":          "2006-01-02 15:04",
	"card_fields":          "all",
	"list_columns":         "key, type, priority, status, points, summary, assignee, marks",
	"card_layout":          "card_fields, in the usual places",
	"card_styles":          "none",
	"home":                 "none",
	"calendar":             "none",
	"meeting_key":          "none",
	"quick_filters":        "none",
	"views":                "none",
	"lane_layouts":         "none",
	"stale_days":           "5",
	"velocity_sprints":     "8",
	"report_done":          "Jira's resolution",
	"report_backwards":     "live",
	"templates":            "none",
	"timer_on_start":       "off",
	"start_assigns":        "off",
	"start_status":         "none",
	"workday_start":        "09:00",
	"capacity":             "none",
	"board_quick_filters":  "on",
	"saved_filters":        "on",
	"remember_filters":     "on",
	"delight":              "on",
	"language":             "en",
	"threaded_replies":     "on",
	"comment_order":        "oldest",
	"comment_layout":       "threaded",
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
	"standup_start":        "everyone",
	"standup_lookback":     "1",
	"standup_length":       "15m",
	"standup_timebox":      "none: the length split",
	"standup_shuffle":      "off",
	"standup_timer":        "off",
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
	{i18n.N("Board and cards"), []string{"default_mode", "card_fields", "list_columns", "card_colors", "empty_lanes", "custom_fields", "card_layout", "card_styles", "lane_layouts", "card_limit", "kanban_done_days", "stale_days", "flag_value", "icons"}},
	{i18n.N("Views and filters"), []string{"home", "quick_filters", "board_quick_filters", "views", "saved_filters", "remember_filters", "filters", "my_work_jql"}},
	{i18n.N("Panel"), []string{"panel_width", "empty_fields", "date_format", "images", "image_max_rows", "templates", "code_theme", "threaded_replies", "comment_order", "comment_layout"}},
	{i18n.N("Refresh"), []string{"auto_refresh", "stale_after", "full_refresh"}},
	{i18n.N("Time and worklogs"), []string{"timer_round", "timer_on_start", "workday_start", "workdays", "capacity", "activity", "calendar", "meeting_key"}},
	{i18n.N("Start work and agents"), []string{"start_assigns", "start_status", "branch_template", "work_branch_template", "work_agent", "work_args", "work_create", "agent_view", "llm", "actions"}},
	{i18n.N("Planning, roadmap and charts"), []string{"velocity_sprints", "report_done", "report_backwards", "roadmap_epic_type", "roadmap_done_days"}},
	{i18n.N("Inbox"), []string{"inbox_every", "inbox_lookback", "inbox_issues"}},
	{i18n.N("Standup"), []string{"standup_start", "standup_lookback", "standup_length", "standup_timebox", "standup_shuffle", "standup_timer"}},
	{i18n.N("Look and feel"), []string{"theme", "mouse", "double_click", "keys", "web_keys", "delight", "skin_tone", "language"}},
	{i18n.N("System"), []string{"open", "clipboard_image", "download_dir", "update_check"}},
}

// SettingDocs is a line on each ui: option, as its config doc says.
var SettingDocs = map[string]string{
	"auto_refresh":         i18n.N("how often an idle board refetches; off stops it"),
	"stale_after":          i18n.N("how old a board may be before focus or a tick refetches it"),
	"images":               i18n.N("inline images in the panel (kitty, Ghostty): auto or off"),
	"image_max_rows":       i18n.N("the tallest an inline image gets, in rows"),
	"card_limit":           i18n.N("the most cards one view fetches"),
	"panel_width":          i18n.N("the issue panel's share of the width, in percent"),
	"keys":                 i18n.N("rebinds actions by name: search: \"/\" or mine: [m, M]; none unbinds one"),
	"web_keys":             i18n.N("rebinds browser keys no terminal action covers, by bind id: \"board:alt+e\": ctrl+e"),
	"default_mode":         i18n.N("the board's mode before one is remembered"),
	"date_format":          i18n.N("a Go time layout for the panel's dates"),
	"calendar":             i18n.N("an iCalendar feed of your meetings (file, https or webcal URL, or a vdir directory): the timesheet proposes them as worklogs, planning takes them off your capacity"),
	"meeting_key":          i18n.N("the issue meetings from ui.calendar are logged on"),
	"home":                 i18n.N("the start screen's widgets, in order: work, inbox, sprint, timer, filters"),
	"card_fields":          i18n.N("what cards show, in order: type, priority, status, points, assignee, avatar, parent, pr, deploy, subtasks, due, flagged, age"),
	"list_columns":         i18n.N("the list view's column order (card_fields picks which show); a header dragged onto another writes it"),
	"card_layout":          i18n.N("where a lane card's fields sit: top, top_right, bottom, bottom_right around the summary"),
	"card_styles":          i18n.N("restyle cards a board query matches: when, edge, tint, fade, bold, hide, show"),
	"quick_filters":        i18n.N("JQL presets shown before every board's own"),
	"views":                i18n.N("JQL-narrowed views of every board, after its own"),
	"lane_layouts":         i18n.N("your own lanes over a board's columns: stacked, split by status, reordered, renamed, hidden; alt+l switches"),
	"stale_days":           i18n.N("days in progress before a card's age shows red"),
	"velocity_sprints":     i18n.N("how many closed sprints the velocity chart shows"),
	"report_done":          i18n.N("the column the charts count as done from, by board id: {\"12\": In review}; d on the charts picks it"),
	"report_backwards":     i18n.N("an issue moved back before the done line: live (no longer done) or first (done since it first got there)"),
	"templates":            i18n.N("the description a new issue starts with, by type (markdown)"),
	"timer_on_start":       i18n.N("start the timer when S starts work on an issue"),
	"start_assigns":        i18n.N("assign the issue to you when S starts work on it"),
	"start_status":         i18n.N("the status S moves the issue to (In Progress); empty for none"),
	"workday_start":        i18n.N("when work logged on another day starts"),
	"capacity":             i18n.N("story points per person a sprint holds; default for everyone else"),
	"board_quick_filters":  i18n.N("the board's own quick filters from Jira; off: only quick_filters"),
	"saved_filters":        i18n.N("your starred Jira filters as views"),
	"remember_filters":     i18n.N("the assignee filter (mine) and each board's quick filters stay on next time; off: every start clears them"),
	"branch_template":      i18n.N("the branch ctrl+y copies: {key} {summary} {type} {project}"),
	"work_branch_template": i18n.N("the branch S creates, same placeholders"),
	"kanban_done_days":     i18n.N("how many days done work stays on a kanban board"),
	"roadmap_epic_type":    i18n.N("the issue type the roadmap shows and creates"),
	"my_work_jql":          i18n.N("the query O's my work view runs"),
	"download_dir":         i18n.N("where attachments are saved"),
	"roadmap_done_days":    i18n.N("how long resolved epics stay on the roadmap"),
	"workdays":             i18n.N("the days you work, for the standup's previous workday and the week's gaps"),
	"inbox_every":          i18n.N("how often the inbox syncs, for the header's count; off stops it"),
	"inbox_lookback":       i18n.N("how far back the inbox reaches"),
	"inbox_issues":         i18n.N("how many recently updated issues the inbox and standup read"),
	"standup_start":        i18n.N("where U opens: everyone, or the first person"),
	"standup_lookback":     i18n.N("how many workdays back the standup starts (1: a Monday covers Friday)"),
	"standup_length":       i18n.N("the whole standup, split over the people for each one's timer"),
	"standup_timebox":      i18n.N("each person's turn instead of the split (2m)"),
	"standup_shuffle":      i18n.N("go round the people in a random order, not the board's"),
	"standup_timer":        i18n.N("a timer for each turn and the whole standup"),
	"timer_round":          i18n.N("rounds the timer's logged time up to a step (15m)"),
	"clipboard_image":      i18n.N("a command printing the clipboard's PNG"),
	"open":                 i18n.N("a command that opens URLs and files"),
	"full_refresh":         i18n.N("how long idle refreshes fetch only changes before a whole refetch (terminal only: the browser always refetches the board)"),
	"filters":              i18n.N("named / queries to recall from the : palette"),
	"card_colors":          i18n.N("how cards show the board's card colours"),
	"mouse":                i18n.N("clicks, drags and the wheel; off leaves the mouse to the terminal"),
	"llm":                  i18n.N("a command answering ctrl+a's questions about the issue, piped on stdin"),
	"delight":              i18n.N("small celebrations: confetti on a card into done, a line on a completed sprint"),
	"language":             i18n.N("the language laneway shows (LANEWAY_LANG overrides it)"),
	"skin_tone":            i18n.N("the skin tone emoji completion (:) offers for people and hands"),
	"threaded_replies":     i18n.N("a reply goes under its comment in Jira's thread (and shows there); off: replies are new comments, quoted"),
	"comment_order":        i18n.N("the Comments tab's order: oldest or newest first (a thread by its first comment, replies oldest first)"),
	"comment_layout":       i18n.N("threaded: replies under their comment; flat: by date, a reply under a line quoting its parent"),
	"update_check":         i18n.N("once a day, whether a newer release exists; never on a dev build"),
	"double_click":         i18n.N("how quickly a second click makes a double-click (100ms–2s)"),
	"icons":                i18n.N("issue type icons: Nerd Font glyphs, or letters for fonts without them"),
	"empty_fields":         i18n.N("every editable field in the panel, or empty ones folded behind a row"),
	"empty_lanes":          i18n.N("board columns with no card after the filters: shown, or hidden for the rest's room; the board's toggle is remembered over it"),
	"custom_fields":        i18n.N("Jira fields by name that cards show and / searches"),
	"flag_value":           i18n.N("the Flagged field's option flagging sets"),
	"work_agent":           i18n.N("the herdr agent kind S launches"),
	"agent_view":           i18n.N("where an attached agent shows: fullscreen or in the panel"),
	"work_args":            i18n.N("the agent's arguments before the start prompt, {key} replaced"),
	"work_create":          i18n.N("a command that makes a missing worktree: {branch}, {base}, {key}"),
	"code_theme":           i18n.N("the chroma style code blocks use (terminal only: the browser's follow its theme)"),
	"theme":                i18n.N("a preset name, or colours by name over an optional preset"),
	"actions":              i18n.N("your own commands on the issue: in the palette, on a key if given"),
	"activity":             i18n.N("commands printing \"time<TAB>key\" lines of a day's work, for the timesheet's proposals"),
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
	case "board_quick_filters", "saved_filters", "remember_filters", "mouse", "delight", "update_check", "threaded_replies":
		return []string{"on", "off"}
	case "language":
		return append([]string{"en"}, i18n.Langs...)
	case "skin_tone":
		return []string{"none", "light", "medium_light", "medium", "medium_dark", "dark"}
	case "timer_on_start", "start_assigns", "standup_shuffle", "standup_timer":
		return []string{"off", "on"}
	case "comment_order":
		return []string{"oldest", "newest"}
	case "comment_layout":
		return []string{"threaded", "flat"}
	case "standup_start":
		return []string{"everyone", "first"}
	case "report_backwards":
		return []string{"live", "first"}
	}
	return nil
}
