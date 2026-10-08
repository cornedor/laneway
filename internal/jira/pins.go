package jira

import "encoding/json"

// PinsMeta is where the field pins are kept (a MetaStore key): field id
// to pinned, for the fields the user pinned or unpinned. A pinned field
// shows on every issue, an unpinned one folds under More; a field with no
// pin keeps its default (the panel's own fields pinned, the rest not).
// Field ids are the site's, so a pin holds in every project.
const PinsMeta = "fields:pins"

// starredMeta is where the pins were kept as stars: the ids pinned.
const starredMeta = "fields:starred"

// FieldPins reads the field pins from s, never nil.
func FieldPins(s MetaStore) map[string]bool {
	pins := map[string]bool{}
	if s == nil {
		return pins
	}
	if v, ok, err := s.GetMeta(PinsMeta); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &pins)
		return pins
	}
	var ids []string
	if v, ok, err := s.GetMeta(starredMeta); err == nil && ok && json.Unmarshal([]byte(v), &ids) == nil {
		for _, id := range ids {
			pins[id] = true
		}
	}
	return pins
}

// SetFieldPin pins field id in s, or unpins it; it returns the pins after.
func SetFieldPin(s MetaStore, id string, on bool) (map[string]bool, error) {
	pins := FieldPins(s)
	pins[id] = on
	b, _ := json.Marshal(pins)
	return pins, s.SetMeta(PinsMeta, string(b))
}

// Pinned is whether field id shows on every issue: its pin, else def.
func Pinned(pins map[string]bool, id string, def bool) bool {
	if on, ok := pins[id]; ok {
		return on
	}
	return def
}
