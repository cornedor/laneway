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

var (
	settingsRestart = config.SettingsRestart
	settingDefaults = config.SettingDefaults
	settingGroups   = config.SettingGroups
)

type settingRow struct {
	name, value, def string // value "" when the file leaves it unset
	group            string
}

// settingRows lists the ui: options by group, each group in its order.
func settingRows(c config.UIConfig) []settingRow {
	v := reflect.ValueOf(c)
	byName := map[string]settingRow{}
	var names []string
	for i := range v.NumField() {
		name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("yaml"), ",")
		byName[name] = settingRow{name: name, value: settingValue(v.Field(i)), def: settingDefaults[name], group: "Other"}
		names = append(names, name)
	}
	var rows []settingRow
	for _, g := range settingGroups {
		for _, n := range g.Names {
			if r, ok := byName[n]; ok {
				r.group = g.Title
				rows = append(rows, r)
				delete(byName, n)
			}
		}
	}
	for _, n := range names { // in no group: last, in the config's order
		if r, ok := byName[n]; ok {
			rows = append(rows, r)
		}
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
	all  []settingRow // every option; rows those the filter keeps
	rows []settingRow
	idx  int
	top  int // the first row shown
	// filter narrows the rows by name and help text (/); finding is while
	// it is typed.
	filter  string
	finding bool
	input   *textinput.Model // the value being edited
	err     string
	// choices are the values of the option being picked, choice the one
	// under the cursor.
	choices []string
	choice  int
}

// window is the first row shown and how many lines the rows and their
// group headings get in height, keeping the cursor's row in them.
func (s *settingsView) window(height int) (top, visible int) {
	visible = max(height-12, 4) // border, padding, title, filter and hint
	s.top = min(s.top, s.idx)
	for s.top < s.idx && s.lines(s.top, s.idx) > visible {
		s.top++
	}
	return s.top, visible
}

// lines is how many lines rows from..to (inclusive) take with their group
// headings.
func (s *settingsView) lines(from, to int) int {
	n := 0
	for i := from; i <= to && i < len(s.rows); i++ {
		if i == from || s.rows[i].group != s.rows[i-1].group {
			n++
		}
		n++
	}
	return n
}

// rowAtLine is the row on line n of the rows shown from first, -1 for a
// group heading or past the end.
func (s *settingsView) rowAtLine(first, n int) int {
	line := 0
	for i := first; i < len(s.rows); i++ {
		if i == first || s.rows[i].group != s.rows[i-1].group {
			if line == n {
				return -1
			}
			line++
		}
		if line == n {
			return i
		}
		line++
	}
	return -1
}

func (m *Model) openSettings() {
	all := settingRows(m.uiConfig)
	m.settings = &settingsView{all: all, rows: all}
}

// applySettingsFilter keeps the options whose name or help has the filter's
// words.
func (s *settingsView) applySettingsFilter() {
	words := strings.Fields(strings.ToLower(s.filter))
	s.rows = nil
	for _, r := range s.all {
		hay := strings.ToLower(r.name + " " + strings.ReplaceAll(r.name, "_", " ") + " " + settingDocs[r.name] + " " + r.group)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) }) {
			s.rows = append(s.rows, r)
		}
	}
	s.idx, s.top = 0, 0
}

// handleSettingsFind types the filter: enter keeps it, esc drops it.
func (m Model) handleSettingsFind(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		s.finding = false
	case "esc":
		s.finding, s.filter = false, ""
		s.applySettingsFilter()
	case "backspace":
		if r := []rune(s.filter); len(r) > 0 {
			s.filter = string(r[:len(r)-1])
			s.applySettingsFilter()
		}
	default:
		if msg.Text != "" {
			s.filter += msg.Text
			s.applySettingsFilter()
		}
	}
	return m, nil
}

func (m Model) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	if s.input != nil {
		return m.handleSettingsInput(msg)
	}
	if s.choices != nil {
		return m.handleSettingsChoice(msg)
	}
	if s.finding {
		return m.handleSettingsFind(msg)
	}
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "/":
		s.finding = true
	case len(s.rows) == 0 && msg.String() != "esc":
	case msg.String() == "enter":
		m.editSetting()
	case msg.String() == "esc" && s.filter != "":
		s.filter = ""
		s.applySettingsFilter()
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
	idx := m.settings.idx
	m.settings.all = settingRows(next)
	m.settings.applySettingsFilter() // the saved value in the filtered rows too
	m.settings.idx = min(idx, max(len(m.settings.rows)-1, 0))
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
	find := jiraDimStyle.Render("/ filter")
	switch {
	case s.finding:
		find = "/" + s.filter + "█"
	case s.filter != "":
		find = jiraKeyStyle.Render("/"+s.filter) + jiraDimStyle.Render(fmt.Sprintf("  %d of %d · esc clears", len(s.rows), len(s.all)))
	}
	lines := []string{helpTitle("Settings", width), find, jiraDimStyle.Render(pad("", nameW) + "  " + pad("value", valW) + "  " + "default")}
	if len(s.rows) == 0 {
		lines = append(lines, jiraDimStyle.Render("no option matches"))
	}
	used := 0
	for i := top; i < len(s.rows); i++ {
		r := s.rows[i]
		if head := i == top || r.group != s.rows[i-1].group; head {
			if used+2 > visible {
				break
			}
			lines = append(lines, jiraViewActive.Render(r.group))
			used++
		} else if used+1 > visible {
			break
		}
		used++
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
	var r settingRow
	if s.idx < len(s.rows) {
		r = s.rows[s.idx]
	}
	if doc := settingDocs[r.name]; doc != "" {
		lines = append(lines, "", jiraDimStyle.Render(truncate(r.name+": "+doc, width)))
	}
	if full := settingFull(m.uiConfig, r.name); r.name != "" && full != "" {
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
	hintText := "↵ edit · / filter · esc closes · writes ui: in " + where
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
