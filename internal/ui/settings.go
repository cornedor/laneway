package ui

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/config"
)

// The settings overlay (,): every ui: option with the value the config
// file gives it and its default.

// settingDefaults is each ui: option's default as the README shows it.
var settingDefaults = map[string]string{
	"auto_refresh":     "2m",
	"stale_after":      "1m",
	"images":           "auto",
	"image_max_rows":   "16",
	"card_limit":       "500",
	"panel_width":      "50",
	"keys":             "as in ?",
	"default_mode":     "lanes",
	"date_format":      "2006-01-02 15:04",
	"card_fields":      "all",
	"quick_filters":    "none",
	"views":            "none",
	"stale_days":       "5",
	"velocity_sprints": "8",
	"templates":        "none",
	"timer_on_start":   "off",
	"capacity":         "none",
	"saved_filters":    "on",
	"branch_template":  defaultBranchTemplate,
	"theme":            "terminal colours",
}

type settingRow struct {
	name, value, def string // value "" when the file leaves it unset
}

// settingRows lists the ui: options in the config's own order.
func settingRows(c config.UIConfig) []settingRow {
	v := reflect.ValueOf(c)
	var rows []settingRow
	for i := range v.NumField() {
		name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("yaml"), ",")
		rows = append(rows, settingRow{name, settingValue(v.Field(i)), settingDefaults[name]})
	}
	return rows
}

// settingValue shows a config value on one line: lists joined, maps and
// lists of records as a count.
func settingValue(f reflect.Value) string {
	switch f.Kind() {
	case reflect.String:
		return f.String()
	case reflect.Int:
		if f.Int() == 0 {
			return ""
		}
		return strconv.FormatInt(f.Int(), 10)
	case reflect.Slice:
		if f.Len() == 0 {
			return ""
		}
		if f.Type().Elem().Kind() == reflect.String {
			return strings.Join(f.Interface().([]string), ", ")
		}
		return fmt.Sprintf("%d set", f.Len())
	case reflect.Map:
		if f.Len() == 0 {
			return ""
		}
		if t, ok := f.Interface().(config.Theme); ok && len(t) == 1 && t["preset"] != "" {
			return t["preset"]
		}
		return fmt.Sprintf("%d set", f.Len())
	}
	return ""
}

type settingsView struct {
	rows []settingRow
	idx  int
}

func (m *Model) openSettings() {
	m.settings = &settingsView{rows: settingRows(m.uiConfig)}
}

func (m Model) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "esc", msg.String() == "q", key.Matches(msg, m.keys.Settings):
		m.settings = nil
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.InputUp):
		s.idx = max(s.idx-1, 0)
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.InputDown):
		s.idx = min(s.idx+1, len(s.rows)-1)
	case key.Matches(msg, m.keys.Home):
		s.idx = 0
	case key.Matches(msg, m.keys.End):
		s.idx = len(s.rows) - 1
	}
	return m, nil
}

func (m *Model) renderSettings(height int) string {
	s := m.settings
	nameW, valW := 0, 0
	for _, r := range s.rows {
		nameW = max(nameW, lipgloss.Width(r.name))
		valW = max(valW, lipgloss.Width(r.value), lipgloss.Width(r.def))
	}
	valW = min(valW, 40)
	visible := max(height-10, 4) // border, padding, title and hint
	top := min(max(s.idx-visible+1, 0), max(len(s.rows)-visible, 0))
	width := nameW + 2 + valW + 2 + valW
	pad := func(v string, w int) string {
		v = truncate(v, w)
		return v + strings.Repeat(" ", w-lipgloss.Width(v))
	}
	lines := []string{helpTitle("Settings", width), jiraDimStyle.Render(pad("", nameW) + "  " + pad("value", valW) + "  " + "default")}
	for i := top; i < min(top+visible, len(s.rows)); i++ {
		r := s.rows[i]
		val := r.value
		if val == "" {
			val = "·"
		}
		line := pad(r.name, nameW) + "  " + pad(val, valW) + "  " + pad(r.def, valW)
		switch {
		case i == s.idx:
			line = selectedRow.Render(line)
		case r.value == "":
			line = pad(r.name, nameW) + "  " + jiraDimStyle.Render(pad(val, valW)+"  "+pad(r.def, valW))
		default:
			line = pad(r.name, nameW) + "  " + jiraKeyStyle.Render(pad(val, valW)) + "  " + jiraDimStyle.Render(pad(r.def, valW))
		}
		lines = append(lines, line)
	}
	where := "ui: in your config file"
	if m.configPath != "" {
		where = "ui: in " + m.configPath
	}
	hint := lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("esc closes · set these under " + where)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).
		Padding(1, 3).Render(lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", hint))
}

// WithConfigPath tells the model which file its config came from.
func (m Model) WithConfigPath(path string) Model {
	m.configPath = path
	return m
}
