package jira

import (
	"encoding/json"
)

// Which fields an issue type's edit screen has follows its project and type
// (the screen scheme), not the issue: Jira serves it per issue (editmeta),
// and serves none at all where the workflow locks the issue (a closed one)
// or the user may not edit it. The last screen seen per project and type is
// kept, so those issues still show their fields, read-only.

// MetaStore keeps small values by key: the state file (internal/store).
type MetaStore interface {
	GetMeta(key string) (string, bool, error)
	SetMeta(key, value string) error
}

// SetLayouts gives the client somewhere to keep the edit screens it saw.
func (c *Client) SetLayouts(s MetaStore) { c.layouts = s }

func layoutKey(project, typeID string) string { return "layout:" + project + ":" + typeID }

// keepLayout remembers fields as project's typeID's edit screen; their
// options are left out, as a read-only field needs none.
func (c *Client) keepLayout(project, typeID string, fields []FieldMeta) {
	if c.layouts == nil || project == "" || typeID == "" || len(fields) == 0 {
		return
	}
	slim := make([]FieldMeta, len(fields))
	for i, f := range fields {
		slim[i] = FieldMeta{ID: f.ID, Name: f.Name, Kind: f.Kind, Clause: f.Clause}
	}
	b, err := json.Marshal(slim)
	if err != nil {
		return
	}
	if old, ok, _ := c.layouts.GetMeta(layoutKey(project, typeID)); !ok || old != string(b) {
		_ = c.layouts.SetMeta(layoutKey(project, typeID), string(b)) // a convenience: failing it only forgets
	}
}

// layout is the edit screen last seen for project's typeID, read-only.
func (c *Client) layout(project, typeID string) []FieldMeta {
	if c.layouts == nil {
		return nil
	}
	v, ok, err := c.layouts.GetMeta(layoutKey(project, typeID))
	if err != nil || !ok {
		return nil
	}
	var fields []FieldMeta
	if json.Unmarshal([]byte(v), &fields) != nil {
		return nil
	}
	for i := range fields {
		fields[i].ReadOnly = true
	}
	return fields
}
