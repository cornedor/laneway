package jira

// Link is an issue related to another: its parent, a subtask, or an issue
// link. Rel says how, from the issue's side ("parent", "subtask", "blocks",
// "is blocked by", …).
type Link struct {
	Rel     string
	Key     string
	Summary string
	Status  string
	LinkID  string // an issue link's id, for DeleteLink; "" for parent and subtasks
}

type apiLinked struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  *named `json:"status"`
	} `json:"fields"`
}

type apiIssueLink struct {
	ID   string `json:"id"`
	Type struct {
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *apiLinked `json:"inwardIssue"`
	OutwardIssue *apiLinked `json:"outwardIssue"`
}

func (a *apiLinked) link(rel string) Link {
	l := Link{Rel: rel, Key: a.Key, Summary: a.Fields.Summary}
	if a.Fields.Status != nil {
		l.Status = a.Fields.Status.Name
	}
	return l
}

// issueLinks flattens parent, links and subtasks, in that order.
func issueLinks(parent *apiLinked, links []apiIssueLink, subtasks []apiLinked) []Link {
	var out []Link
	if parent != nil && parent.Key != "" {
		out = append(out, parent.link("parent"))
	}
	for _, l := range links {
		switch {
		case l.OutwardIssue != nil:
			lk := l.OutwardIssue.link(l.Type.Outward)
			lk.LinkID = l.ID
			out = append(out, lk)
		case l.InwardIssue != nil:
			lk := l.InwardIssue.link(l.Type.Inward)
			lk.LinkID = l.ID
			out = append(out, lk)
		}
	}
	for i := range subtasks {
		out = append(out, subtasks[i].link("subtask"))
	}
	return out
}
