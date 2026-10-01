package jira

// ThreadRoot is the comment a reply to id goes under: Jira's threads are one
// level deep, so a reply to a reply names the reply's own parent (found in
// cs; a missing one, or a loop, stops the walk where it is).
func ThreadRoot(cs []Comment, id string) string {
	parent := map[string]string{}
	for _, c := range cs {
		parent[c.ID] = c.ParentID
	}
	seen := map[string]bool{}
	for !seen[id] {
		seen[id] = true
		p, ok := parent[id]
		if !ok || p == "" {
			return id
		}
		if _, known := parent[p]; !known {
			return p // the parent is a root that is not loaded
		}
		id = p
	}
	return id
}
