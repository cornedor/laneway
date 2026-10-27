package web

import (
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
)

// A pick counted through /users/picked ranks that person first for the
// field, saying why; another field keeps Jira's order.
func TestUsersRanked(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	var us []rankedUser
	if issueCall(t, "GET", ts.URL+"/api/users?issue=DEMO-5&q=&field=customfield_1&name=Tester", nil, &us) != 200 || len(us) < 2 {
		t.Fatalf("users: %v", us)
	}
	last := us[len(us)-1]
	if issueCall(t, "POST", ts.URL+"/api/users/picked", map[string]string{"Where": "DEMO-5", "Field": "customfield_1", "Account": last.AccountID}, nil) != 200 {
		t.Fatal("picked")
	}
	issueCall(t, "GET", ts.URL+"/api/users?issue=DEMO-5&q=&field=customfield_1&name=Tester", nil, &us)
	if us[0].AccountID != last.AccountID || us[0].Why != "frequent" {
		t.Errorf("first = %+v, want %s picked often", us[0], last.DisplayName)
	}
	us = nil
	issueCall(t, "GET", ts.URL+"/api/users?issue=DEMO-5&q=&field=assignee", nil, &us)
	if us[0].AccountID == last.AccountID || us[0].Why != "" {
		t.Errorf("another field ranked by this one's picks: %+v", us[0])
	}
	if issueCall(t, "POST", ts.URL+"/api/users/picked", map[string]string{"Where": "DEMO-5"}, nil) != 400 {
		t.Error("a pick without field and account should be 400")
	}
}
