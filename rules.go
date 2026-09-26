package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

const rulesUsage = `usage: laneway rules list [-config path]
       laneway rules watch [-config path]
       laneway rules test [-config path] [-on kind] [-key K] [-summary S] [-type T]
                          [-status S] [-from-status S] [-assignee A] [-priority P] [-points N] [-by-me B] [-watch JQL]`

// rulesCmd lists the config's rules, or says which a described change
// would fire and what stopped the rest; nothing runs. watch runs them.
func rulesCmd(args []string, out, errOut io.Writer) int {
	if len(args) > 0 && args[0] == "watch" {
		return rulesWatch(args[1:], out, errOut)
	}
	if len(args) == 0 || (args[0] != "list" && args[0] != "test") {
		fmt.Fprintln(errOut, rulesUsage)
		return 2
	}
	fs := flag.NewFlagSet("rules "+args[0], flag.ContinueOnError)
	fs.SetOutput(errOut)
	cfgPath := fs.String("config", "", "config file")
	on := fs.String("on", rules.New, "change kind: new status assignee priority points summary")
	c := jira.Card{}
	var fromStatus string
	fs.StringVar(&c.Key, "key", "TEST-1", "issue key")
	fs.StringVar(&c.Summary, "summary", "", "summary")
	fs.StringVar(&c.Type, "type", "", "issue type (default: rules_test.type, else the project's Task or first type)")
	fs.StringVar(&c.Status, "status", "", "status (default: rules_test.status, else the type's first to-do status)")
	fs.StringVar(&fromStatus, "from-status", "", "the status before, for -on status")
	fs.StringVar(&c.Assignee, "assignee", "", "assignee display name, empty for unassigned")
	fs.StringVar(&c.Priority, "priority", "Medium", "priority")
	fs.StringVar(&c.Points, "points", "", "story points")
	watch := fs.String("watch", "", "the rule watch's JQL that saw the change; empty for a board")
	byMe := fs.String("by-me", "", "true or false: you made the change; unset leaves by_me unknown")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	cfg, path, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	if c.Type == "" || c.Status == "" {
		keySet := false
		fs.Visit(func(f *flag.Flag) { keySet = keySet || f.Name == "key" })
		typ, status := testDefaults(&cfg, keySet, c.Key, c.Type)
		c.Type, c.Status = cmp.Or(c.Type, typ), cmp.Or(c.Status, status)
	}
	set, warn := rules.Compile(cfg.Rules)
	for _, w := range warn {
		fmt.Fprintln(errOut, "skipped:", w)
	}
	fmt.Fprintf(out, "%s: %d rules\n", path, set.Len())
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	if args[0] == "list" {
		for _, r := range set.Rules() {
			on := "any change"
			if len(r.On) > 0 {
				on = strings.Join(r.On, ", ")
			}
			if r.Watch != "" {
				every := r.Every
				if every == "" {
					every = strings.TrimSuffix(rules.DefaultEvery.String(), "0s")
				}
				on += " of " + r.Watch + " every " + every
			}
			var acts []string
			for _, a := range r.Actions {
				acts = append(acts, a.Type)
			}
			fmt.Fprintf(tw, "  %s\ton %s\t%s\n", name(r.Name), on, strings.Join(acts, ", "))
		}
		return 0
	}
	if c.Assignee != "" {
		c.AssigneeID = "test"
	}
	ev := rules.Event{Kind: *on, Card: c, Old: c}
	ev.Old.Status = fromStatus
	ev.Watch = *watch
	if *byMe != "" {
		b := *byMe == "true"
		ev.ByMe = &b
	}
	if *on == rules.New {
		ev.Old = jira.Card{}
	}
	fired := set.Fire(ev)
	all := set.Rules()
	for i, x := range set.Explain(ev) {
		if x.Text != "" {
			fmt.Fprintf(tw, "  ✗ %s\t%s\n", name(x.Rule), x.Text)
			continue
		}
		for len(fired) > 0 && fired[0].Rule == x.Rule {
			f := fired[0]
			fired = fired[1:]
			fmt.Fprintf(tw, "  ✓ %s\t%s\t%s\n", name(f.Rule), f.Action, describe(f))
		}
		if ev.ByMe == nil || *ev.ByMe {
			for _, a := range all[i].Actions {
				if rules.JiraAction(a.Type) {
					fmt.Fprintf(tw, "  · %s\t%s\tonly on others' changes (-by-me=false)\n", name(x.Rule), a.Type)
				}
			}
		}
	}
	return 0
}

// testDefaults is the issue type and status a test assumes when its flags
// leave them out: the config's rules_test, else the project's (the -key's,
// or the first configured) Task or first type and that type's first to-do
// status, else Task and To Do. typ, when set, is the type to find a status
// for.
func testDefaults(cfg *config.Config, keySet bool, key, typ string) (string, string) {
	t, s := cfg.RulesTest.Type, cfg.RulesTest.Status
	if typ != "" {
		t = typ
	}
	if t != "" && s != "" {
		return t, s
	}
	project, _, _ := strings.Cut(key, "-")
	if !keySet {
		project = ""
		if len(cfg.Jira.Projects) > 0 {
			project = cfg.Jira.Projects[0]
		}
	}
	jt, js := projectDefaults(cfg, project, t)
	return cmp.Or(t, jt, "Task"), cmp.Or(s, js, "To Do")
}

// projectDefaults reads project's statuses: its Task or first type (typ
// when given) and that type's first to-do status. Unreachable, it says
// nothing.
func projectDefaults(cfg *config.Config, project, typ string) (string, string) {
	timeout, err := cfg.Jira.RequestTimeout()
	if project == "" || err != nil {
		return "", ""
	}
	c := jira.New(jira.Config{BaseURL: cfg.Jira.BaseURL, Email: cfg.Jira.Email, APIToken: cfg.Jira.APIToken, Timeout: timeout})
	if !c.Enabled() {
		return "", ""
	}
	types, err := c.ProjectStatuses(context.Background(), project)
	if err != nil {
		return "", ""
	}
	i := slices.IndexFunc(types, func(t jira.TypeStatuses) bool { return typ != "" && strings.EqualFold(t.Type, typ) })
	if i < 0 && typ == "" {
		i = slices.IndexFunc(types, func(t jira.TypeStatuses) bool { return strings.EqualFold(t.Type, "Task") })
		if i < 0 {
			i = slices.IndexFunc(types, func(t jira.TypeStatuses) bool { return !t.Subtask })
		}
	}
	if i < 0 {
		return "", ""
	}
	st := types[i].Statuses
	j := slices.IndexFunc(st, func(s jira.ProjectStatus) bool { return s.Category == "new" })
	switch {
	case j >= 0:
		return types[i].Type, st[j].Name
	case len(st) > 0:
		return types[i].Type, st[0].Name
	}
	return types[i].Type, ""
}

func name(n string) string {
	if n == "" {
		return "(unnamed)"
	}
	return n
}

// describe is what a fired action would do.
func describe(f rules.Firing) string {
	switch f.Action {
	case "notify":
		return f.Title + ": " + f.Text
	case "exec":
		return strings.Join(f.Argv, " ")
	case "transition":
		return "→ " + f.To
	case "highlight":
		if f.Color != "" {
			return f.Color
		}
		return "theme highlight"
	}
	return f.Text
}
