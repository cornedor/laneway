package people

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/jira"
)

// Lookup finds the Jira users matching q, a name or an email: the picker's
// own search.
type Lookup func(ctx context.Context, q string) ([]jira.User, error)

// matchCap is how many candidates are looked up in Jira, the best first.
const matchCap = 8

// Match finds the forge's candidates in Jira and returns their scores by
// source and account. A candidate is found through known (jira.git_people:
// a forge username or git email to an account id or a Jira name), a match
// remembered in st, its email, else its name when exactly one person bears
// it or it finds one person. One not found is left out.
func Match(ctx context.Context, cands []forge.Candidate, lookup Lookup, known map[string]string, st Store) map[string]map[string]float64 {
	cands = append([]forge.Candidate(nil), cands...)
	total := func(c forge.Candidate) (t float64) {
		for _, s := range c.Scores {
			t += s
		}
		return t
	}
	sort.SliceStable(cands, func(i, j int) bool { return total(cands[i]) > total(cands[j]) })
	if len(cands) > matchCap {
		cands = cands[:matchCap]
	}
	ids := make([]string, len(cands))
	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Go(func() { ids[i] = account(ctx, c, lookup, known, st) })
	}
	wg.Wait()
	out := map[string]map[string]float64{}
	for i, c := range cands {
		if ids[i] == "" {
			continue
		}
		for src, s := range c.Scores {
			if out[src] == nil {
				out[src] = map[string]float64{}
			}
			out[src][ids[i]] += s
		}
	}
	return out
}

// identities are the keys c is known by: its username and its email.
func identities(c forge.Candidate) []string {
	var out []string
	for _, s := range []string{c.Username, c.Email} {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func account(ctx context.Context, c forge.Candidate, lookup Lookup, known map[string]string, st Store) string {
	keys := identities(c)
	for k, v := range known {
		for _, id := range keys {
			if !strings.EqualFold(k, id) {
				continue
			}
			v = strings.TrimSpace(v)
			if accountID(v) {
				return v
			}
			if strings.Contains(v, "@") {
				return find(ctx, v, lookup, false)
			}
			return find(ctx, v, lookup, true)
		}
	}
	if st != nil {
		for _, id := range keys {
			if v, ok, err := st.GetMeta("people.git." + id); err == nil && ok && v != "" {
				return v
			}
		}
	}
	got := ""
	if c.Email != "" {
		got = find(ctx, c.Email, lookup, false)
	}
	if got == "" && c.Name != "" {
		got = find(ctx, c.Name, lookup, true)
	}
	if got != "" && st != nil {
		for _, id := range keys {
			_ = st.SetMeta("people.git."+id, got)
		}
	}
	return got
}

// accountID is whether s is a Jira Cloud account id rather than a name:
// 5b10ac8d82e05b22cc7d4ef5, 712020:4c5c….
func accountID(s string) bool {
	return len(s) >= 20 && !strings.ContainsAny(s, " @")
}

// find is the one account q names: a search with one result (by name, of
// four letters or more), or, byName, the one result whose name is q.
func find(ctx context.Context, q string, lookup Lookup, byName bool) string {
	us, err := lookup(ctx, q)
	if err != nil {
		return ""
	}
	if len(us) == 1 && (!byName || len([]rune(q)) >= 4) { // a git handle (SBernards) finds its one person
		return us[0].AccountID
	}
	if !byName {
		return ""
	}
	found := ""
	for _, u := range us {
		if strings.EqualFold(strings.TrimSpace(u.DisplayName), q) {
			if found != "" {
				return "" // two people by that name
			}
			found = u.AccountID
		}
	}
	return found
}
