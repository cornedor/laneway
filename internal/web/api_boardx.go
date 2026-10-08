package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// Board extras: views from ui.views and starred filters, the board's own
// card colours, and status history for the time machine.

type cardColorsOut struct {
	jira.CardColors
	// Keys are the issues a custom (JQL) colouring takes, key → #rrggbb.
	Keys map[string]string
}

func init() {
	get("/filters/favourite", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.Client().FavouriteFilters(ctx)
	})
	get("/boards/{board}/cardcolors", boardCardColors)
	get("/boards/{board}/viewcards", viewCards)
	post("/cards/statusmoves", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Keys []string }](r)
		if err != nil {
			return nil, err
		}
		for _, k := range b.Keys {
			if !jira.ValidKey(k) {
				return nil, badRequest(i18n.T("bad issue key"))
			}
		}
		return s.Client().StatusMoves(ctx, b.Keys)
	})
}

// boardCardColors: ?scope= narrows a custom colouring's queries.
func boardCardColors(ctx context.Context, s *Server, r *http.Request) (any, error) {
	id, err := boardID(r)
	if err != nil {
		return nil, err
	}
	cc, err := s.Client().CardColors(ctx, id)
	if err != nil {
		return nil, err
	}
	out := cardColorsOut{CardColors: cc}
	if cc.By == "custom" && len(cc.Colors) > 0 {
		if out.Keys, err = s.Client().CardColorKeys(ctx, cc, Q(r, "scope")); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// viewCards: a configured view. ?kind=jql narrows the board's issues by
// ?jql=, kind=filter searches all of Jira with it; ?filter= (the quick
// filters) is ANDed in either way. The response is {cards, total}.
func viewCards(ctx context.Context, s *Server, r *http.Request) (any, error) {
	id, err := boardID(r)
	if err != nil {
		return nil, err
	}
	c := s.Client()
	q := andOrderedJQL(Q(r, "jql"), Q(r, "filter"))
	if Q(r, "kind") == "filter" {
		cards, err := c.SearchCards(ctx, q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"cards": cards, "total": len(cards)}, nil
	}
	pf := Q(r, "points")
	if pf == "" {
		if cfg, e := c.BoardConfiguration(ctx, id); e == nil {
			pf = cfg.PointsField
		}
	}
	cards, total, err := c.BoardIssues(ctx, id, q, pf)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cards": cards, "total": total}, nil
}

// andOrderedJQL ANDs two clauses, keeping an ORDER BY of the first last.
func andOrderedJQL(q, b string) string {
	where, order := q, ""
	if i := strings.LastIndex(strings.ToUpper(q), "ORDER BY"); i >= 0 {
		where, order = strings.TrimSpace(q[:i]), " "+q[i:]
	}
	switch {
	case where == "":
		return strings.TrimSpace(b + order)
	case b == "":
		return where + order
	}
	return "(" + where + ") AND (" + b + ")" + order
}
