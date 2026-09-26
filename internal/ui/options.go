package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/styles"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// options are the config's ui: section with defaults filled in.
type options struct {
	autoRefresh     time.Duration // 0: off
	staleAfter      time.Duration
	images          bool
	imageMaxRows    int
	panelPct        int
	panelDefault    int  // ui.panel_width, which a drag near it snaps back to
	cardLimit       int  // 0: the client's default
	lanes           bool // default mode
	dateFormat      string
	fields          cardFields
	quick           []jira.QuickFilter // config presets, ids -1, -2, …
	views           []jiraView         // config JQL views
	savedFilters    bool               // starred Jira filters as views
	capacity        map[string]float64 // sprint points per person, "default" for the rest
	timerOnStart    bool               // S also starts the timer
	templates       map[string]string  // new issue descriptions by type, lower-cased
	velocitySprints int                // closed sprints in the velocity chart
	staleDays       int                // in progress longer than this shows red
	branchTemplate  string             // copy_branch's name
	workBranch      string             // start work's new branch
	workAgent       string             // the herdr agent kind start work launches
	kanbanDoneDays  int                // done work older than this leaves kanban boards
	codeTheme       string             // chroma style for code blocks
}

// cardFields is what a card or list row shows besides key and summary.
type cardFields struct {
	typ, priority, status, points, assignee, parent, pr, deploy, subtasks, due, flagged, age, avatar bool
	stale                                                                                            int // ui.stale_days, for age
}

var allCardFields = cardFields{true, true, true, true, true, true, true, true, true, true, true, true, true, 5}

func defaultOptions() options {
	return options{autoRefresh: 2 * time.Minute, staleAfter: time.Minute, images: true, imageMaxRows: 16, panelPct: 50, panelDefault: 50,
		lanes: true, dateFormat: "2006-01-02 15:04", fields: allCardFields, savedFilters: true, velocitySprints: 8, staleDays: 5, branchTemplate: defaultBranchTemplate, workBranch: defaultWorkBranch, workAgent: "claude", kanbanDoneDays: defaultKanbanDoneDays, codeTheme: fallbackCodeTheme}
}

// presetCodeTheme is the chroma style matching each theme preset.
var presetCodeTheme = map[string]string{
	"tokyonight": "tokyonight-night", "catppuccin": "catppuccin-mocha", "gruvbox": "gruvbox",
}

// optionsFrom resolves c over the defaults. A bad value is reported and
// the default kept, so a typo never stops the app.
func optionsFrom(c config.UIConfig) (options, []string) {
	o := defaultOptions()
	var warn []string
	dur := func(name, v string, dst *time.Duration, allowOff bool) {
		switch v = strings.TrimSpace(v); {
		case v == "":
		case allowOff && (v == "off" || v == "0"):
			*dst = 0
		default:
			d, err := time.ParseDuration(v)
			if err != nil || d < 5*time.Second {
				warn = append(warn, fmt.Sprintf("ui.%s: %q is not a duration of 5s or more", name, v))
				return
			}
			*dst = d
		}
	}
	dur("auto_refresh", c.AutoRefresh, &o.autoRefresh, true)
	dur("stale_after", c.StaleAfter, &o.staleAfter, false)
	for name, pts := range c.Capacity {
		if pts < 0 {
			warn = append(warn, fmt.Sprintf("ui.capacity.%s: %v is below 0", name, pts))
			continue
		}
		if o.capacity == nil {
			o.capacity = map[string]float64{}
		}
		o.capacity[name] = pts
	}
	switch n := c.StaleDays; {
	case n == 0:
	case n < 1:
		warn = append(warn, fmt.Sprintf("ui.stale_days: %d is below 1", n))
	default:
		o.staleDays = n
	}
	switch n := c.KanbanDoneDays; {
	case n == 0:
	case n < 1 || n > 365:
		warn = append(warn, fmt.Sprintf("ui.kanban_done_days: %d is not 1–365", n))
	default:
		o.kanbanDoneDays = n
	}
	switch n := c.VelocitySprints; {
	case n == 0:
	case n < 1 || n > 50:
		warn = append(warn, fmt.Sprintf("ui.velocity_sprints: %d is not 1–50", n))
	default:
		o.velocitySprints = n
	}
	for typ, md := range c.Templates {
		if o.templates == nil {
			o.templates = map[string]string{}
		}
		o.templates[strings.ToLower(typ)] = md
	}
	switch name := strings.TrimSpace(c.CodeTheme); {
	case name == "":
		if t, ok := presetCodeTheme[c.Theme["preset"]]; ok {
			o.codeTheme = t
		}
	case styles.Registry[name] == nil:
		warn = append(warn, fmt.Sprintf("ui.code_theme: unknown style %q", name))
	default:
		o.codeTheme = name
	}
	if tmpl := strings.TrimSpace(c.BranchTemplate); tmpl != "" {
		if bad := badBranchPlaceholder(tmpl); bad != "" {
			warn = append(warn, fmt.Sprintf("ui.branch_template: unknown %s", bad))
		} else {
			o.branchTemplate, o.workBranch = tmpl, tmpl
		}
	}
	if a := strings.TrimSpace(c.WorkAgent); a != "" {
		o.workAgent = a
	}
	if tmpl := strings.TrimSpace(c.WorkBranchTemplate); tmpl != "" {
		if bad := badBranchPlaceholder(tmpl); bad != "" {
			warn = append(warn, fmt.Sprintf("ui.work_branch_template: unknown %s", bad))
		} else {
			o.workBranch = tmpl
		}
	}
	switch strings.ToLower(strings.TrimSpace(c.TimerOnStart)) {
	case "", "off":
	case "on":
		o.timerOnStart = true
	default:
		warn = append(warn, fmt.Sprintf("ui.timer_on_start: %q is not on or off", c.TimerOnStart))
	}
	switch strings.ToLower(strings.TrimSpace(c.SavedFilters)) {
	case "", "on":
	case "off":
		o.savedFilters = false
	default:
		warn = append(warn, fmt.Sprintf("ui.saved_filters: %q is not on or off", c.SavedFilters))
	}
	switch strings.ToLower(strings.TrimSpace(c.Images)) {
	case "", "auto":
	case "off":
		o.images = false
	default:
		warn = append(warn, fmt.Sprintf("ui.images: %q is not auto or off", c.Images))
	}
	switch n := c.ImageMaxRows; {
	case n == 0:
	case n < 1 || n > 200:
		warn = append(warn, fmt.Sprintf("ui.image_max_rows: %d is not 1–200", n))
	default:
		o.imageMaxRows = n
	}
	switch n := c.PanelWidth; {
	case n == 0:
	case n < 20 || n > 80:
		warn = append(warn, fmt.Sprintf("ui.panel_width: %d is not 20–80", n))
	default:
		o.panelPct = n
	}
	switch n := c.CardLimit; {
	case n == 0:
	case n < 50 || n > 5000:
		warn = append(warn, fmt.Sprintf("ui.card_limit: %d is not 50–5000", n))
	default:
		o.cardLimit = n
	}
	switch strings.ToLower(strings.TrimSpace(c.DefaultMode)) {
	case "", "lanes":
	case "list":
		o.lanes = false
	default:
		warn = append(warn, fmt.Sprintf("ui.default_mode: %q is not lanes or list", c.DefaultMode))
	}
	if f := strings.TrimSpace(c.DateFormat); f != "" {
		o.dateFormat = f
	}
	for i, q := range c.QuickFilters {
		if strings.TrimSpace(q.Name) == "" || strings.TrimSpace(q.JQL) == "" {
			warn = append(warn, fmt.Sprintf("ui.quick_filters[%d]: needs name and jql", i))
			continue
		}
		o.quick = append(o.quick, jira.QuickFilter{ID: -1 - len(o.quick), Name: q.Name, JQL: q.JQL})
	}
	for i, v := range c.Views {
		if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.JQL) == "" {
			warn = append(warn, fmt.Sprintf("ui.views[%d]: needs name and jql", i))
			continue
		}
		o.views = append(o.views, jiraView{kind: jiraViewJQL, name: v.Name, jql: v.JQL, lanes: true})
	}
	if c.CardFields != nil {
		var f cardFields
		for _, name := range c.CardFields {
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "type":
				f.typ = true
			case "priority":
				f.priority = true
			case "status":
				f.status = true
			case "points":
				f.points = true
			case "assignee":
				f.assignee = true
			case "parent":
				f.parent = true
			case "pr":
				f.pr = true
			case "deploy":
				f.deploy = true
			case "subtasks":
				f.subtasks = true
			case "due":
				f.due = true
			case "flagged":
				f.flagged = true
			case "age":
				f.age = true
			case "avatar":
				f.avatar = true
			default:
				warn = append(warn, fmt.Sprintf("ui.card_fields: unknown field %q", name))
			}
		}
		o.fields = f
	}
	o.fields.stale = o.staleDays
	o.panelDefault = o.panelPct
	return o, warn
}
