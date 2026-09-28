package demo

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The JQL the app sends, well enough for the generated project: clauses
// joined by AND each narrow; ones it doesn't know are ignored. The
// standup's "key in (…) OR (…)" is the one OR it reads.

var (
	orderByRe  = regexp.MustCompile(`(?i)\s+order\s+by\s+.*$`)
	andRe      = regexp.MustCompile(`(?i)\s+and\s+`)
	listRe     = regexp.MustCompile(`\(([^)]*)\)`)
	sprintEqRe = regexp.MustCompile(`^sprint\s*=\s*(\d+)$`)
	dateRe     = regexp.MustCompile(`"?(\d{4}-\d\d-\d\d)`)
	quotedRe   = regexp.MustCompile(`"([^"]*)"`)
)

// search is every issue jql matches, closed sprints' too when it names one.
func (s *Server) search(jql string) []*issue {
	var all []*issue
	for _, k := range s.order {
		all = append(all, s.issues[k])
	}
	low := strings.ToLower(jql)
	if sprintEqRe.MatchString(strings.TrimSpace(orderByRe.ReplaceAllString(low, ""))) || strings.Contains(low, "closedsprints()") {
		for _, k := range s.hidden {
			all = append(all, s.issues[k])
		}
	}
	return s.filter(all, jql)
}

// filter keeps the issues jql matches.
func (s *Server) filter(issues []*issue, jql string) []*issue {
	jql = strings.TrimSpace(orderByRe.ReplaceAllString(jql, ""))
	if jql == "" {
		return issues
	}
	low := strings.ToLower(jql)
	if strings.HasPrefix(low, "key in (") && strings.Contains(low, ") or (") {
		keys := listOf(jql[:strings.Index(low, ") or (")+1])
		rest := s.filter(issues, jql[strings.Index(low, ") or (")+5:])
		return slices.DeleteFunc(slices.Clone(issues), func(i *issue) bool {
			return !slices.Contains(keys, i.key) && !slices.Contains(rest, i)
		})
	}
	out := issues
	for _, clause := range andRe.Split(unwrap(jql), -1) {
		if keep := s.clause(unwrap(clause)); keep != nil {
			out = slices.DeleteFunc(slices.Clone(out), func(i *issue) bool { return !keep(i) })
		}
	}
	return out
}

// unwrap drops the parentheses around all of s: "(a = 1)", not
// "currentUser()"'s.
func unwrap(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		depth := 0
		for i, r := range s {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 && i < len(s)-1 {
				return s // the first ( closes before the end
			}
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func listOf(s string) []string {
	m := listRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	var out []string
	for _, v := range strings.Split(m[1], ",") {
		out = append(out, strings.Trim(strings.TrimSpace(v), `"'`))
	}
	return out
}

// clause is what one clause keeps; nil for one it ignores.
func (s *Server) clause(c string) func(*issue) bool {
	low := strings.ToLower(c)
	field, _, _ := strings.Cut(low, " ")
	field = strings.TrimRight(field, "=!~<>")
	negate := strings.Contains(low, "!=") || strings.Contains(low, " not in ")
	in := func(vals []string, v string) bool {
		return slices.ContainsFunc(vals, func(x string) bool { return strings.EqualFold(x, v) }) != negate
	}
	vals := listOf(c)
	if vals == nil {
		if _, v, ok := strings.Cut(c, "="); ok {
			vals = []string{strings.Trim(strings.TrimSpace(v), `"'`)}
		}
	}
	switch field {
	case "sprint":
		switch {
		case strings.Contains(low, "opensprints()"):
			return func(i *issue) bool { return (i.sprint == activeID || i.sprint == nextID) != negate }
		case strings.Contains(low, "closedsprints()"):
			return func(i *issue) bool { return (i.sprint != 0 && i.sprint < activeID) != negate }
		case strings.Contains(low, "is empty"):
			return func(i *issue) bool { return i.sprint == 0 }
		}
		return func(i *issue) bool { return in(vals, strconv.Itoa(i.sprint)) }
	case "key", "issuekey":
		return func(i *issue) bool { return in(vals, i.key) }
	case "issuetype", "type":
		return func(i *issue) bool { return in(vals, i.typ) }
	case "parent", `"epic link"`:
		return func(i *issue) bool { return in(vals, i.parent) }
	case "status":
		return func(i *issue) bool { return in(vals, i.status.name) }
	case "statuscategory":
		names := map[string]string{"new": "To Do", "indeterminate": "In Progress", "done": "Done"}
		return func(i *issue) bool { return in(vals, names[i.status.cat]) }
	case "labels":
		return func(i *issue) bool {
			return slices.ContainsFunc(i.labels, func(l string) bool { return in(vals, l) }) != negate
		}
	case "flagged":
		return func(i *issue) bool { return i.flagged == strings.Contains(low, "not") }
	case "assignee":
		switch {
		case strings.Contains(low, "is empty"):
			return func(i *issue) bool { return i.assignee == nil }
		case strings.Contains(low, "currentuser()"):
			return func(i *issue) bool { return (i.assignee != nil && *i.assignee == me) != negate }
		}
		return func(i *issue) bool {
			return i.assignee != nil && (in(vals, i.assignee.id) || in(vals, i.assignee.name))
		}
	case "text", "summary":
		m := quotedRe.FindStringSubmatch(c)
		if m == nil {
			return nil
		}
		words := strings.Fields(strings.ToLower(strings.ReplaceAll(m[1], "*", "")))
		return func(i *issue) bool {
			hay := strings.ToLower(i.key + " " + i.summary + " " + i.description)
			for _, w := range words {
				if !strings.Contains(hay, w) {
					return false
				}
			}
			return true
		}
	case "worklogdate":
		d := dateRe.FindStringSubmatch(c)
		if d == nil {
			return nil
		}
		after := strings.Contains(low, ">")
		return func(i *issue) bool {
			return slices.ContainsFunc(i.worklogs, func(w worklog) bool {
				on := w.started.Format("2006-01-02")
				if after {
					return on >= d[1]
				}
				return on < d[1]
			})
		}
	case "issue":
		if strings.Contains(low, "updatedby(") {
			return func(i *issue) bool {
				return i.assignee != nil && *i.assignee == me || slices.ContainsFunc(i.comments, func(c comment) bool { return c.author == me }) ||
					len(i.worklogs) > 0
			}
		}
	}
	return nil
}
