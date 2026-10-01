package jira

import "testing"

func TestThreadRoot(t *testing.T) {
	cs := []Comment{{ID: "1"}, {ID: "2", ParentID: "1"}, {ID: "3", ParentID: "2"}, {ID: "4", ParentID: "9"}, {ID: "5", ParentID: "6"}, {ID: "6", ParentID: "5"}}
	for id, want := range map[string]string{"1": "1", "2": "1", "3": "1", "4": "9", "7": "7"} {
		if got := ThreadRoot(cs, id); got != want {
			t.Errorf("ThreadRoot(%s) = %s, want %s", id, got, want)
		}
	}
	if got := ThreadRoot(cs, "5"); got != "5" && got != "6" {
		t.Errorf("a loop = %s", got)
	}
}
