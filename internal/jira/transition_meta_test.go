package jira

import (
	"encoding/json"
	"testing"
)

func TestFieldMetaHintAndDefault(t *testing.T) {
	var f rawFieldMeta
	json.Unmarshal([]byte(`{"name":"Assignee","schema":{"type":"user"},"description":" Who does it ","hasDefaultValue":true,"defaultValue":{"accountId":"a1"}}`), &f)
	fm := f.meta("assignee")
	if fm.Hint != "Who does it" || string(fm.Default) != `{"accountId":"a1"}` {
		t.Errorf("hint %q default %s", fm.Hint, fm.Default)
	}
	// Jira's usual answer: no description, no default.
	json.Unmarshal([]byte(`{"name":"Assignee","schema":{"type":"user"},"hasDefaultValue":false}`), &f)
	f.Description, f.DefaultValue = "", nil
	if fm := f.meta("assignee"); fm.Hint != "" || fm.Default != nil {
		t.Errorf("Jira field got %q %s", fm.Hint, fm.Default)
	}
}
