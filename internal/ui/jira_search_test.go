package ui

import (
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

func TestJiraQuery(t *testing.T) {
	a := jira.Card{Key: "ABC-1", Summary: "Fix login page", Status: "Code review", Assignee: "Ada Lovelace",
		Priority: "High", Points: "3", Labels: "ui frontend", ParentSummary: "Checkout", Flagged: true}
	b := jira.Card{Key: "ABC-2", Summary: "Docs", Status: "To Do", Priority: "Low", Points: "1"}
	for q, want := range map[string][2]bool{
		"":                       {true, true},
		"login":                  {true, false},
		"fix page":               {true, false}, // every word
		`"fix page"`:             {false, false},
		`"login page"`:           {true, false},
		"status:review":          {true, false},
		"status:review,to":       {true, true},
		"-status:review":         {false, true},
		"points>2":               {true, false},
		"points<=1":              {false, true},
		"prio>=high":             {true, false},
		"prio<medium":            {false, true},
		"assignee:ada,bob":       {true, false},
		"assignee:":              {false, true},
		"epic:":                  {false, true},
		"-epic:":                 {true, false},
		"label:ui":               {true, false},
		"-label:ui":              {false, true},
		"is:flagged":             {true, false},
		"is:unassigned":          {false, true},
		"status=code review":     {false, false}, // two words: status=code and review
		`status="code review"`:   {true, false},
		"nope:x":                 {false, false}, // unknown field: text
		"status:review points>2": {true, false},
	} {
		terms := jiraParseQuery(q)
		if got := [2]bool{jiraCardMatches(a, terms, jiraQueryEnv{}), jiraCardMatches(b, terms, jiraQueryEnv{})}; got != want {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}

func TestJiraQueryDates(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	env := jiraQueryEnv{me: "a1", now: now}
	soon := jira.Card{Key: "ABC-1", AssigneeID: "a1", Updated: now.Add(-time.Hour), Due: now.Add(2 * day), InProgress: true, Since: now.Add(-5 * day), PR: "OPEN", Deploy: "production"}
	late := jira.Card{Key: "ABC-2", Updated: now.Add(-10 * day), Due: now.Add(-day), InProgress: true, Since: now.Add(-time.Hour), PR: "MERGED"}
	done := jira.Card{Key: "ABC-3", Due: now.Add(-day), Done: true}
	for q, want := range map[string][3]bool{
		"is:mine":         {true, false, false},
		"is:overdue":      {false, true, false},
		"due<7d":          {true, true, false},
		"due>1d":          {true, false, false},
		"age>3d":          {true, false, false},
		"age<1d":          {false, true, false},
		"age<12h":         {false, true, false},
		"age>x":           {false, false, false},
		"pr:open":         {true, false, false},
		"pr:open,merged":  {true, true, false},
		"deploy:prod":     {true, false, false},
		"-deploy:":        {true, false, false},
		"is:mine,overdue": {true, true, false},
	} {
		terms := jiraParseQuery(q)
		got := [3]bool{jiraCardMatches(soon, terms, env), jiraCardMatches(late, terms, env), jiraCardMatches(done, terms, env)}
		if got != want {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}
