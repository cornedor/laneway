package index

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// JQL answers the part of q the index can: its top-level AND clauses on
// project, key, status, type, priority, assignee (currentUser() is me, an
// accountId) and statusCategory, is EMPTY on the assignee, and text or
// summary ~ words. The rest it leaves out, returned as dropped; leaving out
// a clause joined by AND only widens the answer, so nothing Jira would find
// is missing. ORDER BY is ignored: most recently updated first, up to n.
func (ix *Index) JQL(q, me string, n int) (hits []Hit, dropped []string, err error) {
	if ix == nil {
		return nil, nil, nil
	}
	where, args := []string{"1"}, []any{}
	for _, cl := range andClauses(stripOrder(q)) {
		w, a, ok := jqlClause(cl, me)
		if !ok {
			dropped = append(dropped, cl)
			continue
		}
		where, args = append(where, w), append(args, a...)
	}
	rows, err := ix.db.Query("SELECT card, synced FROM issues WHERE "+strings.Join(where, " AND ")+" ORDER BY updated DESC LIMIT ?", append(args, n)...)
	if err != nil {
		return nil, dropped, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var ms int64
		if err := rows.Scan(&raw, &ms); err != nil {
			return nil, dropped, err
		}
		h := Hit{Synced: time.UnixMilli(ms)}
		if json.Unmarshal([]byte(raw), &h.Card) == nil {
			hits = append(hits, h)
		}
	}
	return hits, dropped, rows.Err()
}

var orderBy = regexp.MustCompile(`(?i)\s*\border\s+by\b`)

// stripOrder is q without its ORDER BY, outside quotes.
func stripOrder(q string) string {
	for _, loc := range orderBy.FindAllStringIndex(q, -1) {
		if !inQuotes(q, loc[0]) {
			return q[:loc[0]]
		}
	}
	return q
}

var andWord = regexp.MustCompile(`(?i)\s+and\s+`)

// andClauses splits q at the ANDs outside quotes and parentheses; a
// clause wrapped in parentheses whole loses them.
func andClauses(q string) []string {
	var out []string
	add := func(cl string) {
		cl = strings.TrimSpace(cl)
		for strings.HasPrefix(cl, "(") && strings.HasSuffix(cl, ")") && balanced(cl[1:len(cl)-1]) {
			cl = strings.TrimSpace(cl[1 : len(cl)-1])
		}
		if cl != "" {
			out = append(out, cl)
		}
	}
	start := 0
	for _, loc := range andWord.FindAllStringIndex(q, -1) {
		if head := q[start:loc[0]]; !inQuotes(q, loc[0]) && balanced(q[:loc[0]]) {
			add(head)
			start = loc[1]
		}
	}
	add(q[start:])
	return out
}

// balanced is whether s's parentheses pair up, outside quotes.
func balanced(s string) bool {
	depth, quote := 0, rune(0)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			if depth--; depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// inQuotes is whether q's byte at i sits inside a quoted string.
func inQuotes(q string, i int) bool {
	quote := rune(0)
	for _, r := range q[:i] {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		}
	}
	return quote != 0
}

var (
	jqlCompare = regexp.MustCompile(`(?i)^(\w+)\s*(!=|=|not\s+in|in)\s*(.+)$`)
	jqlEmpty   = regexp.MustCompile(`(?i)^assignee\s+is\s+(not\s+)?(empty|null)$`)
	jqlText    = regexp.MustCompile(`(?i)^(text|summary)\s*~\s*(.+)$`)
)

// jqlColumns are the fields a clause compares, as SQL on the issues table.
var jqlColumns = map[string]string{
	"project":   "project",
	"key":       "key",
	"issuekey":  "key",
	"status":    "status",
	"type":      "json_extract(card, '$.Type')",
	"issuetype": "json_extract(card, '$.Type')",
	"priority":  "json_extract(card, '$.Priority')",
	"assignee":  "assignee",
}

// jqlClause is one clause as SQL, ok false when the index can't answer it.
func jqlClause(cl, me string) (where string, args []any, ok bool) {
	if m := jqlEmpty.FindStringSubmatch(cl); m != nil {
		if m[1] != "" {
			return "assignee != ''", nil, true
		}
		return "assignee = ''", nil, true
	}
	if m := jqlText.FindStringSubmatch(cl); m != nil {
		vals, ok := jqlValues(strings.TrimSpace(m[2]), false)
		if !ok {
			return "", nil, false
		}
		var w []string
		for _, word := range strings.Fields(strings.ToLower(vals[0])) {
			w = append(w, `lower(summary) LIKE ? ESCAPE '\'`)
			args = append(args, "%"+strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(word)+"%")
		}
		return "(" + strings.Join(append(w, "1"), " AND ") + ")", args, true
	}
	m := jqlCompare.FindStringSubmatch(cl)
	if m == nil {
		return "", nil, false
	}
	field, op := strings.ToLower(m[1]), strings.ToLower(strings.Join(strings.Fields(m[2]), " "))
	vals, ok := jqlValues(strings.TrimSpace(m[3]), op == "in" || op == "not in")
	if !ok {
		return "", nil, false
	}
	not := op == "!=" || op == "not in"
	if field == "statuscategory" {
		var cats []string
		for _, v := range vals {
			switch strings.ToLower(v) {
			case "done":
				cats = append(cats, "done = 1")
			case "to do", "new":
				cats = append(cats, "(done = 0 AND json_extract(card, '$.InProgress') IS NOT 1)")
			case "in progress", "indeterminate":
				cats = append(cats, "json_extract(card, '$.InProgress') = 1")
			default:
				return "", nil, false
			}
		}
		w := "(" + strings.Join(cats, " OR ") + ")"
		if not {
			w = "NOT " + w
		}
		return w, nil, true
	}
	col, known := jqlColumns[field]
	if !known {
		return "", nil, false
	}
	var ors []string
	for _, v := range vals {
		switch {
		case field == "assignee" && strings.EqualFold(v, "currentUser()"):
			if me == "" {
				return "", nil, false
			}
			ors = append(ors, "json_extract(card, '$.AssigneeID') = ?")
			args = append(args, me)
		case field == "assignee":
			ors = append(ors, "(lower(assignee) = lower(?) OR json_extract(card, '$.AssigneeID') = ?)")
			args = append(args, v, v)
		case strings.Contains(v, "("): // a function the index can't run
			return "", nil, false
		default:
			ors = append(ors, "lower("+col+") = lower(?)")
			args = append(args, v)
		}
	}
	w := "(" + strings.Join(ors, " OR ") + ")"
	if not {
		w = "NOT " + w
	}
	return w, args, true
}

// jqlValues reads a value, or with list a parenthesised list of them.
func jqlValues(s string, list bool) ([]string, bool) {
	if !list {
		if strings.HasPrefix(s, "(") && !strings.EqualFold(s, "currentUser()") {
			return nil, false
		}
		v := unquote(s)
		// Unquoted, a value is one word: "Done OR x" is more than a value.
		return []string{v}, v != "" && (v != s || !strings.ContainsAny(s, " \t"))
	}
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	var out []string
	for _, v := range splitList(s[1 : len(s)-1]) {
		if v = strings.TrimSpace(v); v == "" {
			return nil, false
		}
		if u := unquote(v); u != v || !strings.ContainsAny(v, " \t") {
			out = append(out, u)
			continue
		}
		return nil, false
	}
	return out, len(out) > 0
}

// splitList splits at the commas outside quotes.
func splitList(s string) []string {
	var out []string
	quote, start := rune(0), 0
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ',':
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
