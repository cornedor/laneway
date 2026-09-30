package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/cornedor/laneway/internal/jira"
)

func init() {
	// GET /search?jql= runs JQL; {cards}.
	get("/search", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		jql := strings.TrimSpace(Q(r, "jql"))
		if jql == "" {
			return nil, badRequest("jql is empty")
		}
		cards, err := s.Client().SearchCards(ctx, jql)
		return map[string]any{"cards": cards}, err
	})
	// GET /find?q=&n= is the text search over every visible issue; {cards}.
	get("/find", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		n, _ := strconv.Atoi(Q(r, "n"))
		if n <= 0 || n > 50 {
			n = 20
		}
		cards, err := s.Client().FindIssues(ctx, Q(r, "q"), n)
		if cards == nil {
			cards = []jira.Card{}
		}
		return map[string]any{"cards": cards}, err
	})
	get("/jql/words", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.Client().JQLAutocomplete(ctx)
	})
	get("/jql/values", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		field := Q(r, "field")
		if field == "" {
			return nil, badRequest("field is empty")
		}
		v, err := s.Client().JQLValues(ctx, field, Q(r, "prefix"))
		if v == nil {
			v = []string{}
		}
		return v, err
	})
	get("/filters", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		f, err := s.Client().FavouriteFilters(ctx)
		if f == nil {
			f = []jira.QuickFilter{}
		}
		return f, err
	})
	post("/filters", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Name, JQL string }](r)
		if err != nil {
			return nil, err
		}
		b.Name, b.JQL = strings.TrimSpace(b.Name), strings.TrimSpace(b.JQL)
		if b.Name == "" || b.JQL == "" {
			return nil, badRequest("name and jql are required")
		}
		return nil, s.Client().SaveFilter(ctx, b.Name, b.JQL)
	})
}
