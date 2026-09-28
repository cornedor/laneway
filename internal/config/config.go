// Package config loads the Jira connection from
// ~/.config/laneway/config.yaml, falling back to the old jiratui name and
// then the jira: section of matterbox's config so an existing setup just works.
package config

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/rules"
)

// JiraConfig is matterbox's jira: section.
type JiraConfig struct {
	BaseURL  string `yaml:"base_url"`
	Email    string `yaml:"email"`
	APIToken string `yaml:"api_token"`
	// APITokenCmd prints the token when api_token is unset:
	// [secret-tool, lookup, service, laneway], [pass, jira], [op, read, …].
	APITokenCmd      []string          `yaml:"api_token_cmd,omitempty"`
	Projects         []string          `yaml:"projects"`
	StoryPointsField string            `yaml:"story_points_field"`
	Repos            map[string]string `yaml:"repos,omitempty"`
	StartPrompt      string            `yaml:"start_prompt,omitempty"`
	// Timeout is one API request's limit ("20s"); the longer actions
	// (a board load, a move) stretch with it. For slow instances.
	Timeout string `yaml:"timeout,omitempty"`
}

// RequestTimeout is Timeout read, 0 (the client's default) when unset.
func (j JiraConfig) RequestTimeout() (time.Duration, error) {
	if strings.TrimSpace(j.Timeout) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(j.Timeout))
	if err != nil || d < time.Second {
		return 0, fmt.Errorf("jira.timeout: %q is not a duration of 1s or more", j.Timeout)
	}
	return d, nil
}

type Config struct {
	Jira JiraConfig `yaml:"jira"`
	// Sites are more Jira instances by name, picked with -site or @ in the
	// app; jira: is the default.
	Sites map[string]JiraConfig `yaml:"sites"`
	UI    UIConfig              `yaml:"ui"`
	// Rules fire on the changes a board refresh shows; see internal/rules.
	Rules []rules.Rule `yaml:"rules"`
	// RulesTest overrides the issue type and status `laneway rules test`
	// assumes, else read from the project.
	RulesTest RulesTest `yaml:"rules_test"`
	// Unknown are warnings about keys the file has that no option reads
	// (a typo), set by Load.
	Unknown []string `yaml:"-"`
}

// RulesTest is `laneway rules test`'s defaults.
type RulesTest struct {
	Type   string `yaml:"type"`
	Status string `yaml:"status"`
}

// UIConfig tunes the app; every field is optional and "" / 0 keeps the
// default (see ui.optionsFrom).
type UIConfig struct {
	// AutoRefresh is how often an idle board refetches ("2m"); "off" stops it.
	AutoRefresh string `yaml:"auto_refresh"`
	// StaleAfter is how old a board may be before focus or a tick refetches it.
	StaleAfter string `yaml:"stale_after"`
	// Images is "auto" (kitty/Ghostty) or "off".
	Images string `yaml:"images"`
	// ImageMaxRows caps an inline image's height in rows.
	ImageMaxRows int `yaml:"image_max_rows"`
	// CardLimit caps the cards one view fetches.
	CardLimit int `yaml:"card_limit"`
	// PanelWidth is the issue panel's share of the width, in percent.
	PanelWidth int `yaml:"panel_width"`
	// Keys rebinds actions by name: search: "/" or mine: [m, M].
	Keys map[string]KeyList `yaml:"keys"`
	// DefaultMode is the board's mode before one is remembered: lanes or list.
	DefaultMode string `yaml:"default_mode"`
	// DateFormat is a Go time layout for the panel's dates.
	DateFormat string `yaml:"date_format"`
	// CardFields picks what cards and list rows show, in any order:
	// type, priority, status, points, assignee, parent.
	CardFields []string `yaml:"card_fields"`
	// QuickFilters are JQL presets shown before every board's own.
	QuickFilters []QuickFilter `yaml:"quick_filters"`
	// Views are JQL-narrowed views of every board, after its own.
	Views []QuickFilter `yaml:"views"`
	// StaleDays is how many days a card may sit in progress before its age
	// shows in red (default 5).
	StaleDays int `yaml:"stale_days"`
	// VelocitySprints is how many closed sprints the velocity chart shows
	// (default 8).
	VelocitySprints int `yaml:"velocity_sprints"`
	// Templates are the description a new issue starts with, by issue
	// type name (markdown).
	Templates map[string]string `yaml:"templates"`
	// TimerOnStart is "on" to start the timer when S starts work on an
	// issue (and none runs), "off" by default.
	TimerOnStart string `yaml:"timer_on_start"`
	// StartAssigns is "on" to assign the issue to you when S starts work
	// on it; StartStatus the status S moves it to ("In Progress"), "" for
	// none. Both off by default.
	StartAssigns string `yaml:"start_assigns"`
	StartStatus  string `yaml:"start_status"`
	// WorkdayStart is when work logged on another day starts ("09:00").
	WorkdayStart string `yaml:"workday_start"`
	// Capacity is story points per person a sprint holds, by display name;
	// "default" for everyone not named. Planning shows who is over.
	Capacity map[string]float64 `yaml:"capacity"`
	// SavedFilters is "on" (your starred Jira filters as views, after
	// Views) or "off".
	SavedFilters string `yaml:"saved_filters"`
	// BranchTemplate is the branch name copy_branch puts on the clipboard:
	// {key}, {summary} (slugged), {type}, {project}; "{key}-{summary}".
	BranchTemplate string `yaml:"branch_template"`
	// WorkBranchTemplate names the branch start work (S) creates, same
	// placeholders; by default BranchTemplate when set, else
	// "issue/{key}-{summary}".
	WorkBranchTemplate string `yaml:"work_branch_template"`
	// KanbanDoneDays is how many days done work stays on a kanban board
	// (default 14).
	KanbanDoneDays int `yaml:"kanban_done_days"`
	// RoadmapEpicType is the issue type the roadmap shows and creates
	// ("Epic"); RoadmapDoneDays how long resolved ones stay (90).
	RoadmapEpicType string `yaml:"roadmap_epic_type"`
	// MyWorkJQL is the query O's my work view runs; "" keeps yours in every
	// project, open or done this week.
	MyWorkJQL string `yaml:"my_work_jql"`
	// DownloadDir is where attachments are saved ("~/Downloads/jira");
	// "" is $XDG_DOWNLOAD_DIR, else ~/Downloads.
	DownloadDir     string `yaml:"download_dir"`
	RoadmapDoneDays int    `yaml:"roadmap_done_days"`
	// Workdays are the days you work, for standup's previous workday:
	// [mon, tue, wed, thu, fri] by default.
	Workdays []string `yaml:"workdays"`
	// InboxEvery is how often the header's inbox count refreshes ("5m",
	// "off"); InboxLookback how far back a first read looks ("24h");
	// InboxIssues how many recently updated issues it reads (30).
	InboxEvery    string `yaml:"inbox_every"`
	InboxLookback string `yaml:"inbox_lookback"`
	InboxIssues   int    `yaml:"inbox_issues"`
	// TimerRound rounds the timer's logged time up to a step ("15m"); by
	// default to the minute.
	TimerRound string `yaml:"timer_round"`
	// ClipboardImage is a command printing the clipboard's PNG, over the
	// wl-paste / xclip / pngpaste probe; Open one that opens URLs and files
	// (target appended), over xdg-open / open / rundll32.
	ClipboardImage string `yaml:"clipboard_image"`
	Open           string `yaml:"open"`
	// FullRefresh is how long idle refreshes fetch only changes before a
	// whole refetch ("10m").
	FullRefresh string `yaml:"full_refresh"`
	// CardColors is how cards show the board's own card colours:
	// "ribbon" (default) or "off".
	CardColors string `yaml:"card_colors"`
	// Mouse is "on" (default): clicks, drags and the wheel; "off" leaves
	// the mouse to the terminal (selecting text, its own links).
	Mouse string `yaml:"mouse"`
	// DoubleClick is how quickly a second click makes a double-click
	// ("400ms"), 100ms to 2s.
	DoubleClick string `yaml:"double_click"`
	// Icons is "nerd" (default: Nerd Font glyphs for issue types) or
	// "plain" (letters, for fonts without them).
	Icons string `yaml:"icons"`
	// EmptyFields is "show" (default: every editable field in the panel)
	// or "hide" (empty ones fold behind a row that shows them).
	EmptyFields string `yaml:"empty_fields"`
	// CustomFields are Jira fields by name that cards and list rows show
	// (values only) and / searches ("test type":e2e).
	CustomFields []string `yaml:"custom_fields"`
	// Filters are named / queries to recall from the : palette.
	Filters []NamedQuery `yaml:"filters"`
	// FlagValue is the Flagged field's option flagging sets ("Impediment").
	FlagValue string `yaml:"flag_value"`
	// WorkAgent is the herdr agent kind start work launches ("claude").
	WorkAgent string `yaml:"work_agent"`
	// CodeTheme is the chroma style code blocks use (monokai, dracula, …);
	// by default the one matching the theme preset.
	CodeTheme string `yaml:"code_theme"`
	// Theme is a preset name (theme: tokyonight) or colours by name, over
	// an optional preset: {preset: nord, accent: "#7aa2f7"}.
	Theme Theme `yaml:"theme"`
	// Actions are your own commands, in the palette and on a key if given.
	Actions []Action `yaml:"actions"`
	// Delight is "on" (small celebrations: confetti on a card into done, a
	// line on a completed sprint) or "off".
	Delight string `yaml:"delight"`
}

// QuickFilter is a named JQL clause, ANDed with the board's query: a quick
// filter when on, a view always.
type QuickFilter struct {
	Name string `yaml:"name"`
	JQL  string `yaml:"jql"`
}

// NamedQuery is a / search query with a name.
// Action is a command of yours run on the selected issue, or the marked
// ones: their JSON on stdin, LANEWAY_KEY (LANEWAY_KEYS) in the env.
type Action struct {
	Name    string   `yaml:"name"`
	Key     string   `yaml:"key,omitempty"`
	Command []string `yaml:"command"`
	// Where is board, panel or both (the default).
	Where string `yaml:"where,omitempty"`
	// Show is status (the output's last line, the default) or pager.
	Show string `yaml:"show,omitempty"`
	// Refresh reloads the board and the issue after.
	Refresh bool `yaml:"refresh,omitempty"`
}

type NamedQuery struct {
	Name  string `yaml:"name"`
	Query string `yaml:"query"`
}

// KeyList is one key or a list of them.
type KeyList []string

func (k *KeyList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*k = KeyList{n.Value}
		return nil
	}
	var l []string
	if err := n.Decode(&l); err != nil {
		return err
	}
	*k = l
	return nil
}

// Theme is colours by name; a lone scalar is {preset: <name>}.
type Theme map[string]string

func (t *Theme) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*t = Theme{"preset": n.Value}
		return nil
	}
	var m map[string]string
	if err := n.Decode(&m); err != nil {
		return err
	}
	*t = m
	return nil
}

// Load reads the first config that exists; path "" uses the defaults.
func Load(path string) (Config, string, error) {
	var candidates []string
	if path != "" {
		candidates = []string{path}
	} else {
		d, err := os.UserConfigDir()
		if err != nil {
			return Config{}, "", err
		}
		candidates = []string{
			filepath.Join(d, "laneway", "config.yaml"),
			filepath.Join(d, "jiratui", "config.yaml"),
			filepath.Join(d, "matterbox", "config.yaml"),
		}
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Config{}, p, err
		}
		var c Config
		if filepath.Base(filepath.Dir(p)) == "matterbox" {
			// Its rules: are chat rules; only jira: and ui: carry over.
			var mb struct {
				Jira JiraConfig `yaml:"jira"`
				UI   UIConfig   `yaml:"ui"`
			}
			err = yaml.Unmarshal(raw, &mb)
			c.Jira, c.UI = mb.Jira, mb.UI
		} else {
			err = yaml.Unmarshal(raw, &c)
			c.Unknown = unknownKeys(raw)
		}
		if err != nil {
			return Config{}, p, fmt.Errorf("%s: %w", p, err)
		}
		if env := os.Getenv("JIRA_API_TOKEN"); env != "" {
			c.Jira.APIToken = env
		}
		return c, p, nil
	}
	return Config{}, candidates[0], fmt.Errorf("%w; run `laneway setup`, or create %s with:\n\n%s\nA token: %s", ErrNoConfig, candidates[0], starterConfig, TokenURL)
}

// ErrNoConfig is Load finding no config file; the path it returns with it
// is where one belongs.
var ErrNoConfig = errors.New("no config found")

// StatePath is where the app keeps its state. A state file left by the old
// jiratui name is copied over on first run.
// Site is the Jira config for site: jira: for "", else sites[site].
func (c Config) Site(site string) (JiraConfig, error) {
	j, ok := c.Jira, true
	if site != "" {
		j, ok = c.Sites[site]
	}
	if !ok {
		return JiraConfig{}, fmt.Errorf("no site %q in sites:", site)
	}
	return j.withToken()
}

// withToken runs api_token_cmd for the token when api_token is unset.
func (j JiraConfig) withToken() (JiraConfig, error) {
	if strings.TrimSpace(j.APIToken) != "" || len(j.APITokenCmd) == 0 {
		return j, nil
	}
	out, err := exec.Command(j.APITokenCmd[0], j.APITokenCmd[1:]...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return j, fmt.Errorf("api_token_cmd %s: %w", j.APITokenCmd[0], err)
	}
	j.APIToken = strings.TrimSpace(string(out))
	return j, nil
}

// SiteFor is the name of the site whose base_url is baseURL ("" is
// jira:), ok false when none is.
func (c Config) SiteFor(baseURL string) (string, bool) {
	same := func(j JiraConfig) bool {
		return strings.EqualFold(strings.TrimRight(strings.TrimSpace(j.BaseURL), "/"), strings.TrimRight(baseURL, "/"))
	}
	if same(c.Jira) {
		return "", true
	}
	for _, name := range slices.Sorted(maps.Keys(c.Sites)) {
		if same(c.Sites[name]) {
			return name, true
		}
	}
	return "", false
}

// BaseURL reads what someone types for their Jira: "acme" is
// https://acme.atlassian.net, a missing scheme is https, and a pasted
// Jira Cloud link (a board, an issue) keeps only its host. A self-hosted
// one keeps its path, which can be the context (/jira).
func BaseURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("type your Jira's name or URL")
	}
	if !strings.ContainsAny(s, "./:") {
		s += ".atlassian.net"
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("%q is not a web address", s)
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	if strings.HasSuffix(strings.ToLower(u.Hostname()), ".atlassian.net") {
		u.Path, u.RawPath = "", ""
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// SiteName suggests a sites: name for baseURL: its first host label
// ("acme" for acme.atlassian.net), lower case.
func SiteName(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	label, _, _ := strings.Cut(u.Hostname(), ".")
	return strings.ToLower(label)
}

// ValidSiteName says whether name can name a site: it names the site's
// state file, so letters, digits, - and _ only.
func ValidSiteName(name string) bool {
	return name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}

// SiteNames are the sites to switch between, "" (jira:) first, then by name.
func (c Config) SiteNames() []string {
	names := slices.Sorted(maps.Keys(c.Sites))
	return append([]string{""}, names...)
}

// SiteStatePath is StatePath for site: its own file, as boards, views and
// caches are per instance.
func SiteStatePath(site string) (string, error) {
	p, err := StatePath()
	if err != nil || site == "" {
		return p, err
	}
	return filepath.Join(filepath.Dir(p), "state-"+site+".json"), nil
}

// LastSite is the site last picked with @, when it is still one of names;
// else "" (jira:).
func LastSite(names []string) string {
	p, err := StatePath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(p), "site"))
	if site := strings.TrimSpace(string(b)); err == nil && slices.Contains(names, site) {
		return site
	}
	return ""
}

// SetLastSite remembers site for the next start without -site.
func SetLastSite(site string) error {
	p, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(filepath.Dir(p), "site"), []byte(site+"\n"), 0o600)
}

func StatePath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, "laneway", "state.json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if old, err := os.ReadFile(filepath.Join(d, "jiratui", "state.json")); err == nil {
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
				_ = os.WriteFile(p, old, 0o600)
			}
		}
	}
	return p, nil
}

// unknownKeys warns about each key in raw no option reads: at the top, in
// jira:, each sites: entry, ui: and rules_test:, with the nearest known
// key when one is close.
func unknownKeys(raw []byte) []string {
	var doc yaml.Node
	if yaml.Unmarshal(raw, &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	var warn []string
	check := func(path string, n *yaml.Node, t reflect.Type) {
		if n == nil || n.Kind != yaml.MappingNode {
			return
		}
		known := yamlKeys(t)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if slices.Contains(known, k) {
				continue
			}
			w := fmt.Sprintf("%s%s: unknown option", path, k)
			if near := nearest(k, known); near != "" {
				w += ", did you mean " + near + "?"
			}
			warn = append(warn, w)
		}
	}
	top := doc.Content[0]
	check("", top, reflect.TypeFor[Config]())
	for i := 0; i+1 < len(top.Content); i += 2 {
		v := top.Content[i+1]
		switch top.Content[i].Value {
		case "jira":
			check("jira.", v, reflect.TypeFor[JiraConfig]())
		case "ui":
			check("ui.", v, reflect.TypeFor[UIConfig]())
		case "rules_test":
			check("rules_test.", v, reflect.TypeFor[RulesTest]())
		case "sites":
			if v.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(v.Content); j += 2 {
					check("sites."+v.Content[j].Value+".", v.Content[j+1], reflect.TypeFor[JiraConfig]())
				}
			}
		}
	}
	return warn
}

// yamlKeys are the keys t's fields read.
func yamlKeys(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// nearest is the known key within two edits of k, "" for none.
func nearest(k string, known []string) string {
	best, bestD := "", 3
	for _, c := range known {
		if d := editDistance(k, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// TokenURL is where an Atlassian API token is made.
const TokenURL = "https://id.atlassian.com/manage-profile/security/api-tokens"

// starterConfig is the least a config needs.
const starterConfig = `jira:
  base_url: https://your-instance.atlassian.net
  email: you@example.com
  api_token: ...   # or JIRA_API_TOKEN
`

// Check says what a site's config lacks to connect: the missing settings
// by name, a base_url without its scheme. site names it ("jira" or
// "sites.club").
func (j JiraConfig) Check(site string) error {
	var missing []string
	for _, f := range []struct{ name, v string }{{"base_url", j.BaseURL}, {"email", j.Email}, {"api_token", j.APIToken}} {
		if strings.TrimSpace(f.v) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		hint := ""
		if slices.Contains(missing, "api_token") {
			hint = " (or api_token_cmd; a token: " + TokenURL + ")"
			if site == "jira" {
				hint = " (or api_token_cmd, JIRA_API_TOKEN; a token: " + TokenURL + ")"
			}
		}
		return fmt.Errorf("%s: set %s%s", site, strings.Join(missing, ", "), hint)
	}
	if u := strings.TrimSpace(j.BaseURL); !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return fmt.Errorf("%s.base_url: %q needs its scheme, e.g. https://%s", site, u, u)
	}
	return nil
}
