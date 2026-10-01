package web

import (
	"io/fs"
	"maps"
	"os"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/ui"
)

// Feature parity with the TUI, by its action names (ui.keys, internal/ui/keys.go).
// Every TUI action has a web binding in static/js/lib/keymap.js, or is in
// tuiOnly with the reason it has none. A web feature the TUI has no action
// for is in webOnly, by bind id (scope:key), with the reason. An entry that
// stops being true fails too, so both lists only hold what still differs.

// tuiOnly: TUI action -> why the web binds no key to it.
var tuiOnly = map[string]string{
	"quit":      "the browser closes its tab",
	"plan_goal": "one form edits the sprint's name, goal and end: plan_rename (E)",
}

// webOnly: web bind id -> why the TUI has no action for it.
var webOnly = map[string]string{
	"board:C":    "list columns: the TUI's list has fixed columns",
	"global:g t": "next theme: the TUI takes its theme from ui.theme",
	"rules:N":    "browser notifications need the browser's permission; the TUI's notify writes the terminal's sequence",
}

// Every ui: option is read by both, or is in terminalOnly with the reason
// the browser ignores it.
var sharedOptions = []string{
	"auto_refresh", "stale_after", "card_limit", "panel_width", "keys", "default_mode",
	"date_format", "card_fields", "quick_filters", "views", "stale_days",
	"velocity_sprints", "templates", "timer_on_start", "start_assigns", "start_status",
	"workday_start", "capacity", "saved_filters", "branch_template", "work_branch_template",
	"kanban_done_days", "roadmap_epic_type", "my_work_jql", "roadmap_done_days", "workdays",
	"inbox_every", "inbox_lookback", "inbox_issues", "timer_round", "card_colors",
	"empty_fields", "custom_fields", "filters", "flag_value", "work_agent", "work_args",
	"work_create", "theme", "actions", "delight", "threaded_replies", "skin_tone",
	"update_check", "llm", "activity",
}

var terminalOnly = map[string]string{
	"images":          "the browser shows images itself",
	"image_max_rows":  "an inline image's height in terminal rows; the page sizes images",
	"clipboard_image": "the browser pastes images itself",
	"open":            "the browser opens links and files itself",
	"mouse":           "the browser always has the mouse",
	"double_click":    "the browser has its own double-click",
	"icons":           "Nerd Font glyphs; the page draws its own icons",
	"full_refresh":    "the browser refetches the whole board each time",
	"code_theme":      "code blocks follow the page theme",
	"download_dir":    "the browser saves downloads where it saves them",
	"agent_view":      "an agent shows in the panel's Terminal tab and on the agents screen, never full screen",
}

func TestParityOptions(t *testing.T) {
	known := map[string]bool{}
	ty := reflect.TypeFor[config.UIConfig]()
	for i := range ty.NumField() {
		name, _, _ := strings.Cut(ty.Field(i).Tag.Get("yaml"), ",")
		known[name] = true
		why, term := terminalOnly[name]
		shared := slices.Contains(sharedOptions, name)
		switch {
		case !shared && !term:
			t.Errorf("ui.%s: read it in the web and add it to sharedOptions, or add it to terminalOnly with the reason", name)
		case shared && term:
			t.Errorf("ui.%s is in sharedOptions and terminalOnly", name)
		case term && why == "":
			t.Errorf("terminalOnly ui.%s needs a reason", name)
		}
	}
	for _, name := range append(slices.Collect(maps.Keys(terminalOnly)), sharedOptions...) {
		if !known[name] {
			t.Errorf("ui.%s is no ui: option", name)
		}
	}
}

func TestParityTUIActions(t *testing.T) {
	web := keymapTable(t)
	mapped := map[string]bool{}
	for _, keys := range web {
		for _, a := range keys {
			mapped[a] = true
		}
	}
	tui := ui.KeyActions()
	for _, a := range slices.Sorted(maps.Keys(tui)) {
		why, skip := tuiOnly[a]
		switch {
		case !mapped[a] && !skip:
			t.Errorf("TUI action %q (%s) has no web binding: map one in lib/keymap.js, or add it to tuiOnly with the reason", a, strings.Join(tui[a], ", "))
		case mapped[a] && skip:
			t.Errorf("TUI action %q is in tuiOnly (%s) but lib/keymap.js maps it: drop the entry", a, why)
		case skip && why == "":
			t.Errorf("tuiOnly %q needs a reason", a)
		}
	}
	for a := range tuiOnly {
		if _, ok := tui[a]; !ok {
			t.Errorf("tuiOnly %q is no TUI action", a)
		}
	}
	for a := range mapped {
		if _, ok := tui[a]; !ok {
			t.Errorf("lib/keymap.js maps to %q, which is no TUI action", a)
		}
	}
}

// screens: each TUI screen (keyScopes) and the web scopes that are it.
var screens = map[string][]string{
	"board": {"board"}, "panel": {"issue", "issue-dev", "issue-notes"}, "planning": {"planning"},
	"roadmap": {"roadmap"}, "charts": {"reports"}, "timesheet": {"work"}, "week": {"work"},
	"standup": {"standup"}, "inbox": {"inbox"}, "agents": {"agents", "terminal"},
}

// everywhere are the web scopes live on every screen.
var everywhere = []string{"global", "timer", "undo", "sites", "ask", "agents-global"}

// elsewhere: screen:action -> why the web's screen does without a TUI
// action the TUI's has.
var elsewhere = map[string]string{
	"board:toggle_panel":    "enter opens the panel, esc closes it (v picks a view)",
	"planning:toggle_panel": "enter opens the panel, esc closes it",
	"roadmap:toggle_panel":  "enter opens the panel, esc closes it",
	"panel:toggle_panel":    "esc closes the panel",
	"board:panel_wider":     "the panel's own < and >, with focus in it",
	"board:panel_narrower":  "the panel's own < and >, with focus in it",
	"panel:next_view":       "1-4 pick the tab; [ and ] step through the issues of the list beside",
	"panel:prev_view":       "1-4 pick the tab; [ and ] step through the issues of the list beside",
	"planning:next_view":    "every sprint is listed, none is picked as the target; [ and ] move the card a sprint on",
	"planning:prev_view":    "every sprint is listed, none is picked as the target; [ and ] move the card a sprint back",
}

func TestParityScreens(t *testing.T) {
	web := keymapTable(t)
	tui := ui.KeyScopes()
	for _, screen := range slices.Sorted(maps.Keys(tui)) {
		scopes, ok := screens[screen]
		if !ok {
			t.Errorf("TUI screen %q has no web scopes in screens", screen)
			continue
		}
		has := map[string]bool{}
		for _, sc := range append(slices.Clone(scopes), everywhere...) {
			for _, a := range web[sc] {
				has[a] = true
			}
		}
		for _, a := range tui[screen] {
			id := screen + ":" + a
			why, skip := elsewhere[id]
			_, nowhere := tuiOnly[a]
			switch {
			case nowhere:
			case !has[a] && !skip:
				t.Errorf("TUI %s has %q, the web's %s has no binding for it: map one in lib/keymap.js, or add %q to elsewhere with the reason", screen, a, strings.Join(scopes, "/"), id)
			case has[a] && skip:
				t.Errorf("elsewhere %q (%s): the web binds it now: drop the entry", id, why)
			}
		}
	}
	for id := range elsewhere {
		screen, a, _ := strings.Cut(id, ":")
		if !slices.Contains(tui[screen], a) {
			t.Errorf("elsewhere %q: the TUI's %s has no %q", id, screen, a)
		}
	}
}

func TestParityKeymapBinds(t *testing.T) {
	binds := webBinds(t)
	for scope, keys := range keymapTable(t) {
		for key, a := range keys {
			if !binds[scope+":"+key] {
				t.Errorf("lib/keymap.js maps %s:%s to %q, but no view binds that key there: a ui.keys remap would do nothing", scope, key, a)
			}
		}
	}
}

func TestParityWebOnly(t *testing.T) {
	binds, web := webBinds(t), keymapTable(t)
	for id, why := range webOnly {
		scope, key, _ := strings.Cut(id, ":")
		switch {
		case why == "":
			t.Errorf("webOnly %q needs a reason", id)
		case !binds[id]:
			t.Errorf("webOnly %q: no view binds that key in that scope: drop or fix the entry", id)
		case web[scope][key] != "":
			t.Errorf("webOnly %q maps to the TUI action %q in lib/keymap.js: drop the entry", id, web[scope][key])
		}
	}
}

var (
	reKeymapBlock = regexp.MustCompile(`(?s)'?([\w-]+)'?:\s*\{(.*?)\}`)
	reKeymapPair  = regexp.MustCompile(`(?:'((?:[^'\\]|\\.)*)'|([\w+]+))\s*:\s*'([a-z_]+)'`)
)

// keymapTable reads lib/keymap.js: scope -> web key -> TUI action.
func keymapTable(t *testing.T) map[string]map[string]string {
	t.Helper()
	b, err := os.ReadFile("static/js/lib/keymap.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	pairs := func(s string) map[string]string {
		out := map[string]string{}
		for _, m := range reKeymapPair.FindAllStringSubmatch(s, -1) {
			k := m[2]
			if m[1] != "" || k == "" {
				k = strings.ReplaceAll(m[1], `\\`, `\`)
			}
			out[k] = m[3]
		}
		return out
	}
	common := between(t, src, "const COMMON = {", "};")
	table := between(t, src, "const TABLE = {", "\n};")
	out := map[string]map[string]string{}
	for _, m := range reKeymapBlock.FindAllStringSubmatch(table, -1) {
		keys := pairs(m[2])
		if strings.Contains(m[2], "...COMMON") {
			for k, a := range pairs(common) {
				if _, ok := keys[k]; !ok {
					keys[k] = a
				}
			}
		}
		out[m[1]] = keys
	}
	if len(out) < 10 {
		t.Fatalf("read %d scopes from lib/keymap.js: has its layout changed?", len(out))
	}
	return out
}

func between(t *testing.T, s, from, to string) string {
	t.Helper()
	_, rest, ok := strings.Cut(s, from)
	if !ok {
		t.Fatalf("lib/keymap.js: no %q", from)
	}
	in, _, _ := strings.Cut(rest, to)
	return in
}

var (
	reRoute     = regexp.MustCompile(`name: '([\w-]+)'.*?(?:key: '([^']+)'.*)?import\('\./([\w-]+\.js)'\)`)
	reScopeVar  = regexp.MustCompile(`(\w+)\s*=\s*(?:app\.)?keys\.scope\('([\w-]+)'`)
	reScopeCall = regexp.MustCompile(`keys\.scope\('([\w-]+)'[^)]*\)\.bind\(\s*('(?:[^'\\]|\\.)*'|\[(?:'(?:[^'\\]|\\.)*'|[^\]'])*\])`)
	reBind      = regexp.MustCompile(`(?:^|[^.\w])(\w+)\.bind\(\s*('(?:[^'\\]|\\.)*'|\[(?:'(?:[^'\\]|\\.)*'|[^\]'])*\]|[A-Za-z_]\w*)\s*,`)
	reString    = regexp.MustCompile(`\b(\w+)\s*=\s*'((?:[^'\\]|\\.)*)'`)
	reExported  = regexp.MustCompile(`export const (\w+)\s*=\s*'((?:[^'\\]|\\.)*)'`)
	reQuoted    = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)
)

// webBinds finds the bind ids (scope:key) the frontend registers. A view's
// own scope is named after its route (views/index.js), other scopes after the
// keys.scope('name') a variable holds; a helper handed a view's scope
// ({ scope }) counts for every view that imports it. A key given by name is
// looked up in the file's own 'string' assignments, then in exported
// consts. Other keys made at run time (loops, ui.actions) are not seen.
func webBinds(t *testing.T) map[string]bool {
	t.Helper()
	root := os.DirFS("static/js")
	srcs := map[string]string{}
	exported := map[string][]string{}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".js" || strings.HasPrefix(p, "vendor/") {
			return err
		}
		b, err := fs.ReadFile(root, p)
		srcs[p] = string(b)
		for _, m := range reExported.FindAllStringSubmatch(string(b), -1) {
			exported[m[1]] = append(exported[m[1]], m[2])
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	route := map[string]string{}
	for _, m := range reRoute.FindAllStringSubmatch(srcs["views/index.js"], -1) {
		route["views/"+m[3]] = m[1]
		if m[2] != "" {
			out["global:g "+m[2]] = true // app.js binds g + each route's key
		}
	}
	if len(route) < 5 {
		t.Fatalf("read %d routes from views/index.js: has its layout changed?", len(route))
	}
	// importers: the routes whose view imports the file.
	importers := func(p string) []string {
		var out []string
		for f, r := range route {
			if strings.Contains(srcs[f], "/"+path.Base(p)+"'") {
				out = append(out, r)
			}
		}
		return out
	}
	add := func(scope, spec string) {
		for _, q := range reQuoted.FindAllStringSubmatch(spec, -1) {
			out[scope+":"+strings.ReplaceAll(q[1], `\\`, `\`)] = true
		}
	}
	for p, src := range srcs {
		vars := map[string]string{}
		for _, m := range reScopeVar.FindAllStringSubmatch(src, -1) {
			vars[m[1]] = m[2]
		}
		strs := map[string][]string{}
		for _, m := range reString.FindAllStringSubmatch(src, -1) {
			strs[m[1]] = append(strs[m[1]], m[2])
		}
		for _, m := range reScopeCall.FindAllStringSubmatch(src, -1) {
			add(m[1], m[2])
		}
		for _, m := range reBind.FindAllStringSubmatch(src, -1) {
			spec := m[2]
			if !strings.ContainsAny(spec[:1], "'[") {
				vals := strs[spec]
				if vals == nil {
					vals = exported[spec]
				}
				spec = "'" + strings.Join(vals, "' '") + "'"
			}
			switch {
			case vars[m[1]] != "":
				add(vars[m[1]], spec)
			case route[p] != "":
				add(route[p], spec)
			case m[1] == "scope" && strings.Contains(src, "{ scope"):
				for _, r := range importers(p) {
					add(r, spec)
				}
			}
		}
	}
	return out
}
