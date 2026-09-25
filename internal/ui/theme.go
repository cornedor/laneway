package ui

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"

	"charm.land/lipgloss/v2"
)

// theme names the app's colours: ANSI numbers ("12") or hex ("#7aa2f7").
// ui.theme overrides them by name, over an optional preset.
type theme map[string]string

func defaultTheme() theme {
	return theme{
		"accent":           "12",  // keys, focus, active view
		"dim":              "241", // secondary text
		"selection_fg":     "15",
		"selection_bg":     "8",   // selected row, board focused
		"selection_idle":   "238", // selected row, panel focused
		"error":            "9",
		"mention":          "9",
		"link":             "12",
		"code":             "14",
		"attachment":       "6",
		"over_limit":       "9", // lane past its WIP limit
		"drop_fg":          "0", // drop target while dragging
		"priority_highest": "1",
		"priority_high":    "9",
		"priority_low":     "4",
		"priority_lowest":  "8",
		"type_bug":         "1", // issue type icons
		"type_story":       "2",
		"type_epic":        "5",
		"type_subtask":     "8",
		"type_other":       "4",  // task and the rest
		"highlight":        "11", // a card a rule highlighted
	}
}

// themePresets are full palettes for ui.theme: <name>, all for dark
// terminals. Missing names keep the ANSI default.
var themePresets = map[string]theme{
	"tokyonight": {
		"accent": "#7aa2f7", "dim": "#565f89", "selection_fg": "#c0caf5",
		"selection_bg": "#33467c", "selection_idle": "#283457",
		"error": "#f7768e", "mention": "#f7768e", "link": "#7aa2f7", "code": "#7dcfff",
		"attachment": "#73daca", "over_limit": "#f7768e", "drop_fg": "#1a1b26",
		"priority_highest": "#db4b4b", "priority_high": "#ff9e64",
		"priority_low": "#7aa2f7", "priority_lowest": "#565f89",
		"type_bug": "#f7768e", "type_story": "#9ece6a", "type_epic": "#bb9af7",
		"type_subtask": "#565f89", "type_other": "#7aa2f7", "highlight": "#e0af68",
	},
	"catppuccin": { // mocha
		"accent": "#89b4fa", "dim": "#6c7086", "selection_fg": "#cdd6f4",
		"selection_bg": "#45475a", "selection_idle": "#313244",
		"error": "#f38ba8", "mention": "#f38ba8", "link": "#89b4fa", "code": "#94e2d5",
		"attachment": "#74c7ec", "over_limit": "#f38ba8", "drop_fg": "#11111b",
		"priority_highest": "#f38ba8", "priority_high": "#fab387",
		"priority_low": "#89b4fa", "priority_lowest": "#6c7086",
		"type_bug": "#f38ba8", "type_story": "#a6e3a1", "type_epic": "#cba6f7",
		"type_subtask": "#6c7086", "type_other": "#89b4fa", "highlight": "#f9e2af",
	},
	"gruvbox": { // dark
		"accent": "#83a598", "dim": "#928374", "selection_fg": "#ebdbb2",
		"selection_bg": "#504945", "selection_idle": "#3c3836",
		"error": "#fb4934", "mention": "#fb4934", "link": "#83a598", "code": "#8ec07c",
		"attachment": "#8ec07c", "over_limit": "#fb4934", "drop_fg": "#282828",
		"priority_highest": "#cc241d", "priority_high": "#fe8019",
		"priority_low": "#83a598", "priority_lowest": "#928374",
		"type_bug": "#fb4934", "type_story": "#b8bb26", "type_epic": "#d3869b",
		"type_subtask": "#928374", "type_other": "#83a598", "highlight": "#fabd2f",
	},
}

// curTheme is the theme applyTheme last set, for the priority marks and type icons.
var curTheme theme

func init() { applyTheme(defaultTheme()) }

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// themeFrom lays the config's colours over the defaults, reporting names
// and values it can't use.
func themeFrom(over map[string]string) (theme, []string) {
	th := defaultTheme()
	var warn []string
	if p, ok := over["preset"]; ok {
		if pt, ok := themePresets[p]; ok {
			maps.Copy(th, pt)
		} else {
			warn = append(warn, fmt.Sprintf("ui.theme: unknown preset %q", p))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(over)) {
		v := over[name]
		if name == "preset" {
			continue
		}
		if _, ok := th[name]; !ok {
			warn = append(warn, fmt.Sprintf("ui.theme: unknown colour %q", name))
			continue
		}
		if n, err := strconv.Atoi(v); !(hexColor.MatchString(v) || err == nil && n >= 0 && n <= 255) {
			warn = append(warn, fmt.Sprintf("ui.theme.%s: %q is not 0–255 or #rrggbb", name, v))
			continue
		}
		th[name] = v
	}
	return th, warn
}

// applyTheme sets every themed colour and style.
func applyTheme(th theme) {
	curTheme = th
	c := func(name string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(th[name])) }
	focusedColor, dimColor = lipgloss.Color(th["accent"]), lipgloss.Color(th["dim"])
	accent, dim := c("accent"), c("dim")

	selectedRow = c("selection_fg").Background(lipgloss.Color(th["selection_bg"]))
	diffTreeSelStyle = lipgloss.NewStyle().Background(lipgloss.Color(th["selection_idle"]))
	scrollbarThumbStyle = accent
	mentionStyle = c("mention").Bold(true)
	attachmentStyle = c("attachment")
	statusStyle = dim

	jiraKeyStyle = accent.Bold(true)
	jiraDimStyle = dim
	jiraOverStyle = c("over_limit").Bold(true)
	jiraDropStyle = c("drop_fg").Bold(true).Background(focusedColor)
	jiraViewActive = accent.Bold(true)
	jiraGhostStyle = dim.Faint(true).Italic(true)

	refKeyStyle = accent.Bold(true)
	refLabelStyle, refDimStyle = dim, dim
	refErrStyle = c("error")

	mdCodeStyle, mdCodeBlockStyle = c("code"), c("code")
	mdCodeOpen = ansiOpenSeq(mdCodeStyle)
	mdFenceStyle, mdQuoteBarStyle = dim, dim
	mdLinkStyle = c("link").Underline(true)
}
