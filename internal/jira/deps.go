package jira

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// DepNode is an issue in a dependency tree, with the issues behind it in
// the tree's direction.
type DepNode struct {
	Key, Summary, Status string
	Done                 bool
	Kids                 []DepNode
	Seen                 bool // already in the tree above: not followed again
}

// depMax is how deep a dependency tree goes.
const depMax = 5

// Dependencies are key's blockers and what it blocks, each followed
// through its own blocks links up to depMax deep.
func (c *Client) Dependencies(ctx context.Context, key string) (root DepNode, blockedBy, blocks []DepNode, err error) {
	if !c.Enabled() {
		return DepNode{}, nil, nil, errNotConfigured
	}
	got := map[string]depIssue{}
	fetch := func(k string) (depIssue, error) {
		if d, ok := got[k]; ok {
			return d, nil
		}
		d, err := c.depIssue(ctx, k)
		got[k] = d
		return d, err
	}
	var walk func(k string, up bool, path map[string]bool, depth int) ([]DepNode, error)
	walk = func(k string, up bool, path map[string]bool, depth int) ([]DepNode, error) {
		d, err := fetch(k)
		if err != nil {
			return nil, err
		}
		next := d.blocks
		if up {
			next = d.blockedBy
		}
		var out []DepNode
		for _, n := range next {
			if path[n.Key] || depth >= depMax {
				n.Seen = path[n.Key]
				out = append(out, n)
				continue
			}
			path[n.Key] = true
			kids, err := walk(n.Key, up, path, depth+1)
			delete(path, n.Key)
			if err != nil {
				return nil, err
			}
			n.Kids = kids
			out = append(out, n)
		}
		return out, nil
	}
	d, err := fetch(key)
	if err != nil {
		return DepNode{}, nil, nil, err
	}
	root = d.node
	if blockedBy, err = walk(key, true, map[string]bool{key: true}, 1); err != nil {
		return root, nil, nil, err
	}
	blocks, err = walk(key, false, map[string]bool{key: true}, 1)
	return root, blockedBy, blocks, err
}

// depIssue is one issue and its direct blocks links either way.
type depIssue struct {
	node              DepNode
	blockedBy, blocks []DepNode
}

func (c *Client) depIssue(ctx context.Context, key string) (depIssue, error) {
	var resp struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name     string `json:"name"`
				Category struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
			IssueLinks []struct {
				Type struct {
					Name    string `json:"name"`
					Inward  string `json:"inward"`
					Outward string `json:"outward"`
				} `json:"type"`
				InwardIssue  *depLinked `json:"inwardIssue"`
				OutwardIssue *depLinked `json:"outwardIssue"`
			} `json:"issuelinks"`
		} `json:"fields"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=summary,status,issuelinks"
	if err := c.do(ctx, http.MethodGet, path, key, nil, &resp); err != nil {
		return depIssue{}, err
	}
	f := resp.Fields
	d := depIssue{node: DepNode{Key: resp.Key, Summary: f.Summary, Status: f.Status.Name, Done: f.Status.Category.Key == "done"}}
	for _, l := range f.IssueLinks {
		if !strings.EqualFold(l.Type.Name, "blocks") && !strings.Contains(strings.ToLower(l.Type.Outward), "block") {
			continue
		}
		switch {
		case l.InwardIssue != nil: // it blocks this one
			d.blockedBy = append(d.blockedBy, l.InwardIssue.node())
		case l.OutwardIssue != nil:
			d.blocks = append(d.blocks, l.OutwardIssue.node())
		}
	}
	return d, nil
}

type depLinked struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name     string `json:"name"`
			Category struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
	} `json:"fields"`
}

func (l *depLinked) node() DepNode {
	s := l.Fields.Status
	return DepNode{Key: l.Key, Summary: l.Fields.Summary, Status: s.Name, Done: s.Category.Key == "done"}
}
