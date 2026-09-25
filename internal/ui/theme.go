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
// ui.theme overrides them by name.
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
	}
}

// curTheme is the theme applyTheme last set, for the priority marks.
var curTheme theme

func init() { applyTheme(defaultTheme()) }

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// themeFrom lays the config's colours over the defaults, reporting names
// and values it can't use.
func themeFrom(over map[string]string) (theme, []string) {
	th := defaultTheme()
	var warn []string
	for _, name := range slices.Sorted(maps.Keys(over)) {
		v := over[name]
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
