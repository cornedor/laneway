package ui

import (
	"testing"

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
		if got := [2]bool{jiraCardMatches(a, terms), jiraCardMatches(b, terms)}; got != want {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}
