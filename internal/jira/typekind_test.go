package jira

import (
	"encoding/json"
	"testing"
)

func TestTypeKind(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.atlassian.net/rest/api/2/universal_avatar/view/type/issuetype/avatar/10318?size=medium": "task",
		"https://x.atlassian.net/rest/api/2/universal_avatar/view/type/issuetype/avatar/10303":             "bug",
		"https://jira.example.com/secure/viewavatar?size=xsmall&avatarId=10315&avatarType=issuetype":       "story",
		"https://jira.example.com/secure/viewavatar?avatarId=10307&avatarType=issuetype":                   "epic",
		"https://x.atlassian.net/rest/api/2/universal_avatar/view/type/issuetype/avatar/10316":             "subtask",
		"https://x.atlassian.net/rest/api/2/universal_avatar/view/type/issuetype/avatar/10314":             "",
		"https://x.atlassian.net/rest/api/2/universal_avatar/view/type/project/avatar/10318":               "",
		"http://localhost:8080/avatar/issuetype/10002":                                                     "",
		"": "",
	} {
		if got := TypeKind(in); got != want {
			t.Errorf("TypeKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCardTypeKind(t *testing.T) {
	icon := "https://x.atlassian.net/rest/api/2/universal_avatar/view/type/issuetype/avatar/10318?size=medium"
	f := map[string]json.RawMessage{"issuetype": json.RawMessage(`{"id":"10002","name":"Taak","iconUrl":"` + icon + `"}`)}
	c := toCard("LAN-1", f, "")
	if c.Type != "Taak" || c.TypeKind != "task" || c.TypeAvatar != icon {
		t.Errorf("got %q %q %q", c.Type, c.TypeKind, c.TypeAvatar)
	}
}
