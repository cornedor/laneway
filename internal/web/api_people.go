package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/people"
)

// Person pickers rank who they offer (internal/people): by who was picked
// in the field before, and as its jira.pickers rule says by the issue's
// merge requests, read in the background when the issue opens. The
// terminal's are people_rank.go.

func init() {
	get("/users", users)
	post("/users/picked", usersPicked)
}

// rankedUser is a person a picker offers; Why is why it ranks high, "" when
// it does not.
type rankedUser struct {
	jira.User
	Why string `json:",omitempty"`
}

// peoplePicker ranks s's person pickers.
func (s *Server) peoplePicker() people.Picker {
	p := people.Picker{Rules: s.opt.Jira.Pickers, Known: s.opt.Jira.GitPeople}
	if s.opt.Store != nil {
		p.Store = s.opt.Store
	}
	if s.opt.GitLab != nil {
		p.Forge = s.opt.GitLab
	}
	return p
}

// warmPeople reads key's merge requests for the person pickers in the
// background, so a picker opened later need not wait.
func (s *Server) warmPeople(key string) {
	pp, c := s.peoplePicker(), s.Client()
	if !pp.Wants(key) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
		defer cancel()
		pp.Warm(ctx, key, func(ctx context.Context, q string) ([]jira.User, error) { return c.AssignableUsers(ctx, key, q) })
	}()
}

// usersFor is how long an answer of /users is kept: the web keeps no index
// of people, as the terminal does, and a picker opened twice should not ask
// Jira twice.
const usersFor = 5 * time.Minute

var usersKept sync.Map // site + where + query → usersHit

type usersHit struct {
	us []jira.User
	at time.Time
}

// assignable is AssignableUsers, its answers kept usersFor.
func (s *Server) assignable(ctx context.Context, where, q string) ([]jira.User, error) {
	k := s.opt.Site + "\x00" + where + "\x00" + q
	if v, ok := usersKept.Load(k); ok && time.Since(v.(usersHit).at) < usersFor {
		return v.(usersHit).us, nil
	}
	us, err := s.Client().AssignableUsers(ctx, where, q)
	if err == nil {
		usersKept.Store(k, usersHit{us, time.Now()})
	}
	return us, err
}

// users: ?issue=KEY or ?project=P narrows to who can be assigned; q filters.
// ?field= (an id, "assignee", "reporter") and ?name= rank them for that
// field.
func users(ctx context.Context, s *Server, r *http.Request) (any, error) {
	where := Q(r, "issue")
	if where == "" {
		where = Q(r, "project")
	}
	if where == "" {
		return nil, badRequest(i18n.T("issue or project is required"))
	}
	us, err := s.assignable(ctx, where, Q(r, "q"))
	if err != nil {
		return []rankedUser{}, err
	}
	out := make([]rankedUser, len(us))
	for i, u := range us {
		out[i] = rankedUser{User: u}
	}
	field := Q(r, "field")
	if field == "" {
		return out, nil
	}
	pp := s.peoplePicker()
	ranks := pp.Ranks(where, pp.Sources(people.Project(where), field, Q(r, "name")), field)
	for i := range out {
		if rk, ok := ranks[out[i].AccountID]; ok {
			out[i].Why = rk.Why
		}
	}
	people.Order(out, func(u rankedUser) string { return u.AccountID }, ranks)
	return out, nil
}

// usersPicked counts a pick: {Where (an issue or project key), Field,
// Account}.
func usersPicked(ctx context.Context, s *Server, r *http.Request) (any, error) {
	b, err := Body[struct{ Where, Field, Account string }](r)
	if err != nil {
		return nil, err
	}
	if b.Where == "" || b.Field == "" || b.Account == "" {
		return nil, badRequest(i18n.T("where, field and account are required"))
	}
	if s.opt.Store == nil {
		return map[string]bool{"ok": true}, nil
	}
	return map[string]bool{"ok": true}, people.Record(s.opt.Store, people.Project(b.Where), b.Field, b.Account, time.Now())
}
