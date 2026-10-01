package jira

import (
	"encoding/json"
	"slices"
)

// StarredMeta is where the starred fields are kept (a MetaStore key): the
// fields the panel shows on every issue, the others folded under More.
// Field ids are the site's, so a star holds in every project.
const StarredMeta = "fields:starred"

// Starred reads the starred field ids from s.
func Starred(s MetaStore) []string {
	if s == nil {
		return nil
	}
	v, ok, err := s.GetMeta(StarredMeta)
	var ids []string
	if err != nil || !ok || json.Unmarshal([]byte(v), &ids) != nil {
		return nil
	}
	return ids
}

// SetStarred stars field id in s, or takes its star off; it returns the
// starred ids after.
func SetStarred(s MetaStore, id string, on bool) ([]string, error) {
	ids := slices.DeleteFunc(Starred(s), func(x string) bool { return x == id })
	if on {
		ids = append(ids, id)
	}
	b, _ := json.Marshal(ids)
	return ids, s.SetMeta(StarredMeta, string(b))
}
