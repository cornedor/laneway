// Package rules runs the config's rules: over the changes a board refresh
// shows (a new issue, a status or assignee change, …), like matterbox's
// listen rules over chat events.
package rules

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/jira"
)

// The change kinds a rule can fire on (on:). A rule with no on: fires on
// all of them.
const (
	New      = "new"
	Status   = "status"
	Assignee = "assignee"
	Priority = "priority"
	Points   = "points"
	Summary  = "summary"
)

var kinds = []string{New, Status, Assignee, Priority, Points, Summary}

// Rule is one entry of the config's rules: list.
type Rule struct {
	Name    string   `yaml:"name"`
	On      StrList  `yaml:"on"`
	Match   Match    `yaml:"match"`
	Actions []Action `yaml:"actions"`
}

// Match is what an issue must be for a rule to fire; every set field must
// hold. Globs (*, ?) are case-insensitive and a list matches any entry.
type Match struct {
	Key        StrList `yaml:"key"`
	Type       StrList `yaml:"type"`
	Status     StrList `yaml:"status"`
	FromStatus StrList `yaml:"from_status"` // the status before a status change
	Assignee   StrList `yaml:"assignee"`    // display name; "none" for unassigned
	Priority   StrList `yaml:"priority"`
	Summary    string  `yaml:"summary"` // RE2 regexp
	ByMe       *bool   `yaml:"by_me"`   // made (or created) by you; needs a changelog read
	Not        *Match  `yaml:"not"`
}

// Action is what a firing rule does.
type Action struct {
	Type    string   `yaml:"type"`    // log, notify or exec
	Text    string   `yaml:"text"`    // log line or notification body; "" says what changed
	Title   string   `yaml:"title"`   // notify: the title, "" for laneway
	Command []string `yaml:"command"` // exec: argv
	Color   string   `yaml:"color"`   // highlight: ANSI 0–255 or #rrggbb, "" for the theme's
}

// Every text is a template over Vars: {{.Key}} {{.Summary}} {{.OldStatus}} …

// actionTypes are the actions a rule can take.
var actionTypes = []string{"log", "notify", "exec", "highlight"}

// StrList is one string or a list of them.
type StrList []string

func (l *StrList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = StrList{n.Value}
		return nil
	}
	var s []string
	if err := n.Decode(&s); err != nil {
		return err
	}
	*l = s
	return nil
}

// Event is one change a refresh showed: Card as it is now, Old as it was
// (zero for a new issue).
type Event struct {
	Kind string
	Card jira.Card
	Old  jira.Card
	// ByMe is whether you made the change, nil when not looked up: a by_me
	// condition then does not hold.
	ByMe *bool
}

// Diff lists the changes from old to cur, in cur's order. Issues that left
// the list are not events.
func Diff(old, cur []jira.Card) []Event {
	prev := make(map[string]jira.Card, len(old))
	for _, c := range old {
		prev[c.Key] = c
	}
	var out []Event
	for _, c := range cur {
		o, ok := prev[c.Key]
		if !ok {
			out = append(out, Event{Kind: New, Card: c})
			continue
		}
		add := func(kind string, changed bool) {
			if changed {
				out = append(out, Event{Kind: kind, Card: c, Old: o})
			}
		}
		add(Status, o.StatusID != c.StatusID)
		add(Assignee, o.AssigneeID != c.AssigneeID)
		add(Priority, o.Priority != c.Priority)
		add(Points, o.Points != c.Points)
		add(Summary, o.Summary != c.Summary)
	}
	return out
}

// Set is the compiled rules.
type Set struct {
	rules []compiled
}

type compiled struct {
	Rule
	match *cmatch
	acts  []cact
}

// cact is an Action with its templates parsed.
type cact struct {
	text, title *template.Template
	argv        []*template.Template
}

// cmatch is a Match with its regexp compiled, and its Not.
type cmatch struct {
	*Match
	summary *regexp.Regexp
	not     *cmatch
}

// Compile checks every rule, keeping the good ones and reporting the rest,
// so one typo never disarms the others.
func Compile(rs []Rule) (*Set, []string) {
	s := &Set{}
	var warn []string
	for i, r := range rs {
		c, err := compile(r)
		if err != nil {
			warn = append(warn, fmt.Sprintf("rules[%d] %s: %v", i, r.Name, err))
			continue
		}
		s.rules = append(s.rules, c)
	}
	return s, warn
}

func compile(r Rule) (compiled, error) {
	c := compiled{Rule: r}
	for _, k := range r.On {
		if !slices.Contains(kinds, k) {
			return c, fmt.Errorf("on: %q is not one of %s", k, strings.Join(kinds, ", "))
		}
	}
	var err error
	if c.match, err = compileMatch(&r.Match); err != nil {
		return c, err
	}
	if len(r.Actions) == 0 {
		return c, fmt.Errorf("no actions")
	}
	for _, a := range r.Actions {
		if !slices.Contains(actionTypes, a.Type) {
			return c, fmt.Errorf("unknown action type %q", a.Type)
		}
		if a.Type == "exec" && len(a.Command) == 0 {
			return c, fmt.Errorf("exec needs a command")
		}
		if n, err := strconv.Atoi(a.Color); a.Color != "" && !(hexColor.MatchString(a.Color) || err == nil && n >= 0 && n <= 255) {
			return c, fmt.Errorf("color %q is not 0–255 or #rrggbb", a.Color)
		}
		var ca cact
		var err error
		parse := func(what, src string) *template.Template {
			t, e := template.New("").Option("missingkey=error").Parse(src)
			if e != nil && err == nil {
				err = fmt.Errorf("bad %s template: %v", what, e)
			}
			return t
		}
		ca.text, ca.title = parse("text", a.Text), parse("title", a.Title)
		for _, arg := range a.Command {
			ca.argv = append(ca.argv, parse("command", arg))
		}
		if err != nil {
			return c, err
		}
		c.acts = append(c.acts, ca)
	}
	return c, nil
}

func compileMatch(m *Match) (*cmatch, error) {
	if m == nil {
		return nil, nil
	}
	c := &cmatch{Match: m}
	if m.Summary != "" {
		var err error
		if c.summary, err = regexp.Compile(m.Summary); err != nil {
			return nil, fmt.Errorf("bad summary regexp %q: %v", m.Summary, err)
		}
	}
	for _, globs := range [][]string{m.Key, m.Type, m.Status, m.FromStatus, m.Assignee, m.Priority} {
		for _, g := range globs {
			if _, err := path.Match(strings.ToLower(g), ""); err != nil {
				return nil, fmt.Errorf("bad glob %q", g)
			}
		}
	}
	var err error
	c.not, err = compileMatch(m.Not)
	return c, err
}

// Rules are the compiled rules, as configured.
func (s *Set) Rules() []Rule {
	out := make([]Rule, len(s.rules))
	for i, r := range s.rules {
		out[i] = r.Rule
	}
	return out
}

// UsesByMe reports whether any rule reads by_me, so events need an author.
func (s *Set) UsesByMe() bool {
	for _, r := range s.rules {
		for m := &r.Match; m != nil; m = m.Not {
			if m.ByMe != nil {
				return true
			}
		}
	}
	return false
}

// Len is how many rules compiled.
func (s *Set) Len() int { return len(s.rules) }

// Firing is one action a rule takes on an event, its templates filled.
type Firing struct {
	Rule   string
	Action string
	Text   string   // log line, notification body; for Explain, why it did not fire
	Title  string   // notify
	Argv   []string // exec
	Color  string   // highlight
	Vars   map[string]string
}

// Fire runs the matching rules' actions over ev, in rule order.
func (s *Set) Fire(ev Event) []Firing {
	var out []Firing
	for _, r := range s.rules {
		if r.why(ev) != "" {
			continue
		}
		vars := Vars(ev)
		for i, a := range r.Actions {
			ca := r.acts[i]
			f := Firing{Rule: r.Name, Action: a.Type, Vars: vars, Text: render(ca.text, vars), Color: a.Color}
			if f.Text == "" {
				f.Text = Describe(ev)
			}
			if f.Title = render(ca.title, vars); f.Title == "" {
				f.Title = "laneway"
			}
			for _, t := range ca.argv {
				f.Argv = append(f.Argv, render(t, vars))
			}
			out = append(out, f)
		}
	}
	return out
}

// Explain says, per rule, why ev would not fire it ("" when it would).
func (s *Set) Explain(ev Event) []Firing {
	out := make([]Firing, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, Firing{Rule: r.Name, Text: r.why(ev)})
	}
	return out
}

// why is the first condition ev fails, "" when the rule fires.
func (r compiled) why(ev Event) string {
	if len(r.On) > 0 && !slices.Contains(r.On, ev.Kind) {
		return "on: not " + ev.Kind
	}
	return r.match.why(ev)
}

// why is the first condition of m that ev fails, "" when all hold and its
// not does not.
func (m *cmatch) why(ev Event) string {
	c := ev.Card
	assignee := c.Assignee
	if c.AssigneeID == "" {
		assignee = "none"
	}
	checks := []struct {
		name  string
		globs []string
		v     string
	}{
		{"key", m.Key, c.Key},
		{"type", m.Type, c.Type},
		{"status", m.Status, c.Status},
		{"from_status", m.FromStatus, ev.Old.Status},
		{"assignee", m.Assignee, assignee},
		{"priority", m.Priority, c.Priority},
	}
	for _, ch := range checks {
		if len(ch.globs) > 0 && !anyGlob(ch.globs, ch.v) {
			return fmt.Sprintf("%s: %q", ch.name, ch.v)
		}
	}
	if len(m.FromStatus) > 0 && ev.Kind != Status {
		return "from_status: not a status change"
	}
	if m.summary != nil && !m.summary.MatchString(c.Summary) {
		return "summary: no match"
	}
	if m.ByMe != nil && (ev.ByMe == nil || *ev.ByMe != *m.ByMe) {
		if ev.ByMe == nil {
			return "by_me: author unknown"
		}
		return fmt.Sprintf("by_me: %t", *ev.ByMe)
	}
	if m.not != nil && m.not.why(ev) == "" {
		return "not: matched"
	}
	return ""
}

func anyGlob(globs []string, v string) bool {
	v = strings.ToLower(v)
	for _, g := range globs {
		if ok, _ := path.Match(strings.ToLower(g), v); ok {
			return true
		}
	}
	return false
}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// Vars are what templates, exec's environment and its stdin see.
func Vars(ev Event) map[string]string {
	c, o := ev.Card, ev.Old
	return map[string]string{
		"Kind": ev.Kind, "Key": c.Key, "Summary": c.Summary, "Type": c.Type,
		"Status": c.Status, "Assignee": c.Assignee, "Priority": c.Priority,
		"Points": c.Points, "Parent": c.ParentKey,
		"OldStatus": o.Status, "OldAssignee": o.Assignee,
		"OldPriority": o.Priority, "OldPoints": o.Points,
		"Describe": Describe(ev),
	}
}

func render(t *template.Template, vars map[string]string) string {
	var b bytes.Buffer
	if err := t.Execute(&b, vars); err != nil {
		return "template: " + err.Error()
	}
	return b.String()
}

// Describe is a one-line account of ev: "ABC-1 status To do → Done".
func Describe(ev Event) string {
	c, o := ev.Card, ev.Old
	switch ev.Kind {
	case New:
		return fmt.Sprintf("%s new: %s", c.Key, c.Summary)
	case Status:
		return fmt.Sprintf("%s status %s → %s", c.Key, o.Status, c.Status)
	case Assignee:
		return fmt.Sprintf("%s assignee %s → %s", c.Key, orNone(o.Assignee), orNone(c.Assignee))
	case Priority:
		return fmt.Sprintf("%s priority %s → %s", c.Key, o.Priority, c.Priority)
	case Points:
		return fmt.Sprintf("%s points %s → %s", c.Key, orNone(o.Points), orNone(c.Points))
	}
	return fmt.Sprintf("%s summary: %s", c.Key, c.Summary)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
