package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/config"
)

// The settings overlay (,): every ui: option with the value the config
// file gives it and its default. enter edits a plain one (text, number or
// word list) in place: checked as at startup, written to the file through
// its YAML tree (comments kept) and applied at once.

// settingsRestart are options read once at startup.
var settingsRestart = map[string]bool{"images": true, "image_max_rows": true, "card_limit": true, "default_mode": true, "flag_value": true, "inbox_issues": true, "custom_fields": true}

// settingDefaults is each ui: option's default as the README shows it.
var settingDefaults = map[string]string{
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
	"skin_tone":            "none",
	"update_check":         "on",
	"llm":                  "claude -p, when claude is on the PATH",
	"branch_template":      defaultBranchTemplate,
	"work_branch_template": "branch_template if set, else issue/{key}-{summary}",
	"kanban_done_days":     "14",
	"roadmap_epic_type":    "Epic",
	"my_work_jql":          "yours everywhere, open or done this week",
	"download_dir":         "$XDG_DOWNLOAD_DIR, else ~/Downloads",
	"roadmap_done_days":    "90",
	"workdays":             "mon–fri",
	"inbox_every":          "5m",
	"inbox_lookback":       "24h",
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
	rows  []settingRow
	idx   int
	input *textinput.Model // the value being edited
	err   string
	// choices are the values of the option being picked, choice the one
	// under the cursor.
	choices []string
	choice  int
}

// window is the first row shown and how many show in height.
func (s *settingsView) window(height int) (top, visible int) {
	visible = max(height-10, 4) // border, padding, title and hint
	return min(max(s.idx-visible+1, 0), max(len(s.rows)-visible, 0)), visible
}

func (m *Model) openSettings() {
	m.settings = &settingsView{rows: settingRows(m.uiConfig)}
}

func (m Model) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	if s.input != nil {
		return m.handleSettingsInput(msg)
	}
	if s.choices != nil {
		return m.handleSettingsChoice(msg)
	}
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "enter":
		m.editSetting()
	case msg.String() == "esc", msg.String() == "q", key.Matches(msg, m.keys.Settings):
		m.settings = nil
	case m.formFieldStep(msg) != 0:
		s.idx = min(max(s.idx+m.formFieldStep(msg), 0), len(s.rows)-1)
	default:
		_, visible := s.window(m.bodyH())
		s.idx, _ = m.keys.listNav(msg, s.idx, len(s.rows), visible, false)
	}
	return m, nil
}

// settingField is the UIConfig field for the yaml name.
func settingField(c *config.UIConfig, name string) reflect.Value {
	v := reflect.ValueOf(c).Elem()
	for i := range v.NumField() {
		if n, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("yaml"), ","); n == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// editable is whether a field edits on one line: text, a number, words.
func editable(f reflect.Value) bool {
	switch f.Kind() {
	case reflect.String, reflect.Int:
		return true
	case reflect.Slice:
		return f.Type().Elem().Kind() == reflect.String && f.Type() != reflect.TypeFor[config.KeyList]()
	}
	return false
}

func (m *Model) editSetting() {
	s := m.settings
	r := s.rows[s.idx]
	if choices := settingChoices(r.name); choices != nil {
		s.choices, s.err = choices, ""
		s.choice = max(slices.IndexFunc(choices, func(c string) bool { return strings.EqualFold(c, r.value) }), 0)
		return
	}
	if !editable(settingField(&m.uiConfig, r.name)) {
		s.err = r.name + " holds more than a line: edit it in the file"
		return
	}
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = r.def
	ti.SetValue(r.value)
	ti.SetWidth(40)
	ti.Focus()
	s.input, s.err = &ti, ""
}

// handleSettingsChoice moves through the option's values; enter saves the
// one under the cursor.
func (m Model) handleSettingsChoice(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		s.choices, s.err = nil, ""
	case "enter":
		if err := m.saveSetting(s.rows[s.idx].name, s.choices[s.choice]); err != "" {
			s.err = err
			return m, nil
		}
		s.choices, s.err = nil, ""
	default:
		s.choice, _ = m.keys.listNav(msg, s.choice, len(s.choices), settingChoicesShown, false)
	}
	return m, nil
}

// settingChoicesShown is how many values the picker shows at once.
const settingChoicesShown = 8

func (m Model) handleSettingsInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		s.input, s.err = nil, ""
		return m, nil
	case "enter":
		if err := m.saveSetting(s.rows[s.idx].name, s.input.Value()); err != "" {
			s.err = err
			return m, nil
		}
		s.input, s.err = nil, ""
		return m, nil
	}
	if d := m.formFieldStep(msg); d != 0 { // keep it and move on
		if r := s.rows[s.idx]; s.input.Value() != r.value {
			if err := m.saveSetting(r.name, s.input.Value()); err != "" {
				s.err = err
				return m, nil
			}
		}
		s.input, s.err = nil, ""
		s.idx = min(max(s.idx+d, 0), len(s.rows)-1)
		return m, nil
	}
	var cmd tea.Cmd
	*s.input, cmd = s.input.Update(msg)
	return m, cmd
}

// saveSetting checks text as the option's new value (empty: the default),
// writes it to the config file and applies it; the error is for the user.
func (m *Model) saveSetting(name, text string) string {
	next := m.uiConfig
	f := settingField(&next, name)
	text = strings.TrimSpace(text)
	var value any
	switch f.Kind() {
	case reflect.String:
		f.SetString(text)
		value = text
	case reflect.Int:
		n := 0
		if text != "" {
			var err error
			if n, err = strconv.Atoi(text); err != nil {
				return fmt.Sprintf("%s: %q is not a number", name, text)
			}
		}
		f.SetInt(int64(n))
		value = n
	case reflect.Slice:
		words := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' })
		f.Set(reflect.ValueOf(words))
		value = words
	}
	if f.IsZero() || f.Kind() == reflect.Slice && f.Len() == 0 {
		value = nil
	}
	opts, warn := optionsFrom(next)
	for _, w := range warn {
		if strings.HasPrefix(w, "ui."+name) {
			return w
		}
	}
	if m.configPath == "" {
		return "no config file to write to"
	}
	if err := config.SetUI(m.configPath, name, value); err != nil {
		return err.Error()
	}
	// A dragged panel width stays until the default itself changes.
	if name != "panel_width" && m.opts.panelPct != m.opts.panelDefault {
		opts.panelPct = m.opts.panelPct
	}
	m.uiConfig, m.opts = next, opts
	plainIcons = opts.plainIcons
	setCodeTheme(opts.codeTheme)
	m.settings.rows = settingRows(next)
	m.jiraTab.rows = nil
	m.renderJira()
	if m.refOpen {
		m.renderRef() // code blocks, dates
	}
	m.status = "saved ui." + name
	if settingsRestart[name] {
		m.status += " · takes effect on restart"
	}
	return ""
}

func (m *Model) renderSettings(height int) string {
	s := m.settings
	nameW, valW := 0, 0
	for _, r := range s.rows {
		nameW = max(nameW, lipgloss.Width(r.name))
		valW = max(valW, lipgloss.Width(r.value), lipgloss.Width(r.def))
	}
	valW = min(valW, 40, max((m.width-8-nameW-4)/2, 8)) // the two value columns share what the screen leaves
	top, visible := s.window(height)
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
		case i == s.idx && s.input != nil:
			s.input.SetWidth(max(2*valW, 10))
			line = pad(r.name, nameW) + "  " + s.input.View()
		case i == s.idx:
			line = selectedRow.Render(line)
		case r.value == "":
			line = pad(r.name, nameW) + "  " + jiraDimStyle.Render(pad(val, valW)+"  "+pad(r.def, valW))
		default:
			line = pad(r.name, nameW) + "  " + jiraKeyStyle.Render(pad(val, valW)) + "  " + jiraDimStyle.Render(pad(r.def, valW))
		}
		lines = append(lines, line)
		if i == s.idx && s.choices != nil {
			cur := r.value
			if cur == "" && r.name != "code_theme" && r.name != "theme" {
				cur = s.choices[0] // the default
			}
			first := min(max(s.choice-settingChoicesShown/2, 0), max(len(s.choices)-settingChoicesShown, 0))
			for j := first; j < min(first+settingChoicesShown, len(s.choices)); j++ {
				c := s.choices[j]
				mark := "  "
				if strings.EqualFold(c, cur) {
					mark = "✓ "
				}
				row := strings.Repeat(" ", nameW+2) + mark + c
				if j == s.choice {
					row = strings.Repeat(" ", nameW+2) + selectedRow.Render(mark+c)
				}
				lines = append(lines, row)
			}
		}
	}
	r := s.rows[s.idx]
	if doc := settingDocs[r.name]; doc != "" {
		lines = append(lines, "", jiraDimStyle.Render(truncate(r.name+": "+doc, width)))
	}
	if full := settingFull(m.uiConfig, r.name); full != "" {
		fl := strings.Split(full, "\n")
		if len(fl) > 8 {
			fl = append(fl[:8], "…")
		}
		for _, l := range fl {
			lines = append(lines, jiraKeyStyle.Render("  "+truncate(l, width-2)))
		}
	}
	where := "your config file"
	if m.configPath != "" {
		where = m.configPath
	}
	hintText := "↵ edit · esc closes · writes ui: in " + where
	switch {
	case s.input != nil:
		hintText = "↵ save · tab/↑↓ save and move · empty for the default · esc cancel"
	case s.choices != nil:
		hintText = "↑ ↓ choose · ↵ save · esc cancel"
	}
	hint := lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render(truncate(hintText, max(m.width-8, width)))
	foot := []string{strings.Join(lines, "\n"), "", hint}
	if s.err != "" {
		foot = append(foot, refErrStyle.Render(truncate(s.err, width)))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).
		Padding(1, 3).Render(lipgloss.JoinVertical(lipgloss.Left, foot...))
}

// WithConfigPath tells the model which file its config came from.
func (m Model) WithConfigPath(path string) Model {
	m.configPath = path
	return m
}
