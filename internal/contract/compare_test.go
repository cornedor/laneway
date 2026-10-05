package contract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func outline(t *testing.T, ep, js string) Doc {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(js))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	s := Shape{}
	s.add(".", v)
	return Doc{ep: s}
}

// TestCompare: a key or type Jira never sent is wrong; what Jira only sent
// null, empty or not at all is unverified; allow lets a path through.
func TestCompare(t *testing.T) {
	const ep = "GET /x 200"
	jira := outline(t, ep, `{"a":"s","n":1,"due":null,"list":[],"m":{},"o":{"k":1}}`)
	demo := outline(t, ep, `{"a":1,"n":2,"due":"2026-01-01","list":[{"x":1}],"m":{"GitHub":{}},"o":{"k":1,"extra":true},"new":{"deep":1}}`)
	demo["GET /y 200"] = Shape{".": kObject}

	r := Compare(jira, demo, nil)
	wantWrong := []string{
		"GET /x 200: a is number, Jira sends string",
		"GET /x 200: new, Jira never sent it",
		"GET /x 200: o.extra, Jira never sent it",
	}
	if !reflect.DeepEqual(r.Wrong, wantWrong) {
		t.Errorf("wrong = %q", r.Wrong)
	}
	wantUnverified := []string{
		"GET /x 200: due is string, Jira only sent null",
		"GET /x 200: list[], Jira's list was always empty",
		"GET /x 200: m.GitHub, Jira's m was always empty",
		"GET /y 200: Jira never answered it",
	}
	if !reflect.DeepEqual(r.Unverified, wantUnverified) {
		t.Errorf("unverified = %q", r.Unverified)
	}

	r = Compare(jira, demo, map[string]bool{ep + " | new": true, ep + " | o.extra": true})
	if len(r.Wrong) != 1 {
		t.Errorf("allowed, still wrong: %q", r.Wrong)
	}
	if r = Compare(demo, jira, nil); !reflect.DeepEqual(r.Missing, []string{"GET /y 200"}) {
		t.Errorf("missing = %q", r.Missing)
	}
}

// TestEndpoint: ids, issue keys and project keys fold; the API version stays.
func TestEndpoint(t *testing.T) {
	for path, want := range map[string]string{
		"/rest/api/3/issue/LAN-12/comment":            "GET /rest/api/3/issue/{key}/comment 200",
		"/rest/agile/1.0/board/7/sprint/31/issue":     "GET /rest/agile/1.0/board/{id}/sprint/{id}/issue 200",
		"/rest/api/3/issue/createmeta/LAN/issuetypes": "GET /rest/api/3/issue/createmeta/{project}/issuetypes 200",
	} {
		if got := endpoint("GET", path, 200); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

// TestNamed: custom fields go by type, an unknown one by customfield_*;
// account ids as keys fold.
func TestNamed(t *testing.T) {
	d := outline(t, "GET /x 200", `{"fields":{"customfield_10020":[{"id":1}],"customfield_99999":"x","5b10a2844c20165700ede21f":1}}`)
	got := d.named(map[string]string{"customfield_10020": "gh-sprint"})["GET /x 200"]
	for _, p := range []string{"fields.customfield[gh-sprint][].id", "fields.customfield_*", "fields.*"} {
		if _, ok := got[p]; !ok {
			t.Errorf("no %s in %v", p, got)
		}
	}
}

// TestDocRoundTrip: what Write prints, ReadDoc reads back.
func TestDocRoundTrip(t *testing.T) {
	d := outline(t, "GET /x 200", `{"a":[{"b":null}],"c":"s"}`)
	d["GET /x 200"]["c"] |= kNull
	var buf bytes.Buffer
	if err := d.Write(&buf, "header\ntwo lines"); err != nil {
		t.Fatal(err)
	}
	back, err := ReadDoc(&buf)
	if err != nil || !reflect.DeepEqual(back, d) {
		t.Errorf("round trip = %v, %v; want %v", back, err, d)
	}
}
