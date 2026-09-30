package web

import (
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
)

func TestEditRoutes(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	iu := ts.URL + "/api/issues/DEMO-5"

	var pr []jira.Option
	if issueCall(t, "GET", ts.URL+"/api/priorities", nil, &pr) != 200 || len(pr) < 2 {
		t.Fatalf("priorities: %v", pr)
	}
	var us []jira.User
	if issueCall(t, "GET", ts.URL+"/api/users?issue=DEMO-5&q=", nil, &us) != 200 || len(us) == 0 {
		t.Fatalf("users: %v", us)
	}
	if issueCall(t, "GET", ts.URL+"/api/users", nil, nil) != 400 {
		t.Error("users without issue or project should be 400")
	}

	var cur jira.Issue
	issueCall(t, "GET", iu+"?fresh=1", nil, &cur)
	type reply struct{ Undo *edit }
	put := func(field string, e edit) (int, reply) {
		var r reply
		code := issueCall(t, "PUT", iu+"/field/"+field, e, &r)
		return code, r
	}

	orig := cur.PriorityID
	code, r := put("priority", edit{ID: pr[len(pr)-1].ID})
	if code != 200 || r.Undo == nil || r.Undo.ID != cur.PriorityID {
		t.Fatalf("priority: %d %+v (was %q)", code, r.Undo, cur.PriorityID)
	}
	if code, _ = put("priority", *r.Undo); code != 200 {
		t.Fatalf("priority undo: %d", code)
	}
	issueCall(t, "GET", iu+"?fresh=1", nil, &cur)
	if cur.PriorityID != orig {
		t.Errorf("priority not restored: %q, was %q", cur.PriorityID, orig)
	}

	code, r = put("summary", edit{Text: "  New summary "})
	if code != 200 || r.Undo == nil || r.Undo.Text == "" {
		t.Fatalf("summary: %d %+v", code, r.Undo)
	}
	issueCall(t, "GET", iu+"?fresh=1", nil, &cur)
	if cur.Summary != "New summary" {
		t.Errorf("summary = %q", cur.Summary)
	}
	if code, _ := put("summary", edit{Text: " "}); code != 400 {
		t.Errorf("blank summary: %d", code)
	}

	code, r = put("assignee", edit{ID: us[0].AccountID})
	if code != 200 || r.Undo == nil {
		t.Fatalf("assignee: %d", code)
	}
	code, r = put("labels", edit{Add: []string{"web-test"}})
	if code != 200 || r.Undo == nil || len(r.Undo.Remove) != 1 {
		t.Fatalf("labels: %d %+v", code, r.Undo)
	}
	if code, _ := put("points", edit{Text: "x"}); code == 200 {
		t.Error("non-numeric points accepted")
	}
	if code, _ := put("duedate", edit{Text: "nonsense"}); code != 400 {
		t.Errorf("bad due date: %d", code)
	}
	if code, _ := put("nonsense", edit{}); code != 400 {
		t.Errorf("unknown field: %d", code)
	}

	code, r = put("status", edit{To: "done"})
	if code != 200 || r.Undo == nil || r.Undo.To == "" {
		t.Fatalf("status: %d %+v", code, r.Undo)
	}
	if code, _ = put("status", edit{To: "no such status"}); code != 400 {
		t.Errorf("unknown status: %d", code)
	}
}

func TestEditMetaAndCreate(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	iu := ts.URL + "/api/issues/DEMO-5"

	var tm struct{ Transitions []moveOption }
	if issueCall(t, "GET", iu+"/transitionmeta", nil, &tm) != 200 || len(tm.Transitions) == 0 {
		t.Fatalf("transitionmeta: %+v", tm)
	}
	var types struct{ Types []jira.Option }
	if issueCall(t, "GET", ts.URL+"/api/projects/DEMO/issuetypes", nil, &types) != 200 || len(types.Types) == 0 {
		t.Fatalf("issuetypes: %+v", types)
	}
	var cf []jira.CreateField
	if issueCall(t, "GET", ts.URL+"/api/projects/DEMO/createfields?type="+types.Types[0].Name, nil, &cf) != 200 || len(cf) == 0 {
		t.Fatalf("createfields: %+v", cf)
	}
	var em map[string]any
	if code := issueCall(t, "GET", iu+"/editmeta", nil, &em); code != 200 {
		t.Fatalf("editmeta: %d", code)
	}

	var made struct{ Key string }
	body := map[string]any{"Project": "DEMO", "Type": types.Types[0].Name, "Summary": "From the web", "Description": "hello"}
	if issueCall(t, "POST", ts.URL+"/api/issues", body, &made) != 200 || made.Key == "" {
		t.Fatalf("create: %+v", made)
	}
	if issueCall(t, "POST", ts.URL+"/api/issues", map[string]any{"Project": "DEMO"}, nil) == 200 {
		t.Error("create without summary should fail")
	}

	var res struct {
		Done   []string
		Failed map[string]string
		Undo   map[string]edit
	}
	code := issueCall(t, "POST", ts.URL+"/api/bulk", map[string]any{"Keys": []string{"DEMO-5", "DEMO-6", "NOPE"}, "Edit": edit{Field: "labels", Add: []string{"bulk"}}}, &res)
	if code != 200 || len(res.Done) != 2 || len(res.Failed) != 1 || len(res.Undo) != 2 {
		t.Fatalf("bulk: %d %+v", code, res)
	}
	code = issueCall(t, "POST", ts.URL+"/api/bulk", map[string]any{"Each": res.Undo}, &res)
	if code != 200 || len(res.Done) != 2 {
		t.Fatalf("bulk undo: %d %+v", code, res)
	}
	if issueCall(t, "POST", ts.URL+"/api/bulk", map[string]any{}, nil) != 400 {
		t.Error("empty bulk should be 400")
	}
	if issueCall(t, "DELETE", ts.URL+"/api/issues/DEMO-7", nil, nil) != 200 {
		t.Error("delete failed")
	}
}
