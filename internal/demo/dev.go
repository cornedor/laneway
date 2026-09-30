package demo

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Development work for a few issues: what Jira's dev-status API and the
// Development field say when GitHub, GitLab and a CI provider are linked.

const devField = "customfield_10000"

const ciTool = "cloud-providers" // builds and deployments

type devPR struct {
	n             int
	title, status string
	src, dst      string
	author        user
	reviewers     []user
	approved      []bool
	comments      int
	updated       time.Time
}

type devCommit struct {
	msg    string
	author user
	at     time.Time
}

type devBuild struct {
	n                       int
	state                   string
	ref                     string
	passed, failed, skipped int
	at                      time.Time
}

type devDeploy struct {
	env, typ, state string
	n, secs         int
	at              time.Time
}

type devWork struct {
	tool, repo string // GitHub or GitLab, and the repository
	branch     string
	prs        []devPR
	commits    []devCommit // oldest first; the last is the branch's head
	builds     []devBuild
	deploys    []devDeploy
}

func (s *Server) devWork(key string) *devWork {
	now := s.now
	ago := func(h float64) time.Time { return now.Add(-time.Duration(h * float64(time.Hour))) }
	switch key {
	case "DEMO-4":
		return &devWork{tool: "GitHub", repo: "shop-web", branch: "issue/DEMO-4-guest-checkout",
			prs: []devPR{{n: 418, title: "DEMO-4 Checkout without an account", status: "OPEN", src: "issue/DEMO-4-guest-checkout", dst: "main",
				author: me, reviewers: []user{mira, tomas}, approved: []bool{true, false}, comments: 4, updated: ago(2)}},
			commits: []devCommit{{"Guest session token on the cart", me, ago(70)}, {"Skip the account step behind checkout.guest", me, ago(50)},
				{"Address form for guests", me, ago(26)}, {"Review: keep the upsell after paying", me, ago(3)}},
			builds: []devBuild{{n: 231, state: "in_progress", ref: "issue/DEMO-4-guest-checkout", at: ago(0.2)}}}
	case "DEMO-7":
		return &devWork{tool: "GitLab", repo: "shop-api", branch: "issue/DEMO-7-shipping-rate-cache",
			prs: []devPR{{n: 87, title: "Cache the shipping-rate lookup per region", status: "OPEN", src: "issue/DEMO-7-shipping-rate-cache", dst: "main",
				author: priya, reviewers: []user{sam, me}, approved: []bool{true, true}, comments: 6, updated: ago(3)}},
			commits: []devCommit{{"Rate cache keyed by region and weight band", priya, ago(52)}, {"Expire cached rates at carrier cut-off", priya, ago(30)},
				{"Metrics: cache hit ratio", priya, ago(5)}},
			builds:  []devBuild{{n: 5521, state: "successful", ref: "issue/DEMO-7-shipping-rate-cache", passed: 412, skipped: 3, at: ago(4.5)}},
			deploys: []devDeploy{{env: "staging", typ: "staging", state: "successful", n: 311, secs: 184, at: ago(4)}}}
	case "DEMO-8":
		return &devWork{tool: "GitHub", repo: "shop-web", branch: "issue/DEMO-8-lazy-images",
			prs: []devPR{{n: 415, title: "Lazy-load product images on the order page", status: "OPEN", src: "issue/DEMO-8-lazy-images", dst: "main",
				author: mira, reviewers: []user{priya}, approved: []bool{false}, comments: 2, updated: ago(20)}},
			commits: []devCommit{{"loading=lazy with sized placeholders", mira, ago(46)}, {"Keep the first row eager", mira, ago(21)}},
			builds:  []devBuild{{n: 228, state: "failed", ref: "issue/DEMO-8-lazy-images", passed: 204, failed: 3, at: ago(20.5)}}}
	case "DEMO-11":
		return &devWork{tool: "GitLab", repo: "shop-api", branch: "issue/DEMO-11-guest-confirmation",
			prs: []devPR{{n: 81, title: "Order confirmation e-mail for guests", status: "MERGED", src: "issue/DEMO-11-guest-confirmation", dst: "main",
				author: tomas, reviewers: []user{mira, sam}, approved: []bool{true, true}, comments: 9, updated: ago(130)}},
			commits: []devCommit{{"Guest confirmation template", tomas, ago(200)}, {"Send from the order-placed event", tomas, ago(180)},
				{"Plain-text part", tomas, ago(170)}, {"Review: no tracking pixel", tomas, ago(150)}, {"Link to the order without an account", tomas, ago(140)}},
			builds: []devBuild{{n: 5460, state: "successful", ref: "main", passed: 398, skipped: 3, at: ago(129)}},
			deploys: []devDeploy{{env: "staging", typ: "staging", state: "successful", n: 298, secs: 176, at: ago(128)},
				{env: "production", typ: "production", state: "successful", n: 299, secs: 243, at: ago(100)}}}
	case "DEMO-12":
		return &devWork{tool: "GitLab", repo: "shop-api", branch: "issue/DEMO-12-drop-v1-cart",
			prs: []devPR{{n: 79, title: "Drop the legacy /v1/cart endpoint", status: "MERGED", src: "issue/DEMO-12-drop-v1-cart", dst: "main",
				author: sam, reviewers: []user{priya}, approved: []bool{true}, comments: 3, updated: ago(110)},
				{n: 74, title: "Remove /v1/cart", status: "DECLINED", src: "sam/remove-v1-cart", dst: "main",
					author: sam, reviewers: []user{priya}, comments: 5, updated: ago(260)}},
			commits: []devCommit{{"Remove /v1/cart and its tests", sam, ago(140)}, {"410 Gone for old app versions", sam, ago(120)}},
			builds:  []devBuild{{n: 5472, state: "successful", ref: "main", passed: 380, skipped: 3, at: ago(109)}},
			deploys: []devDeploy{{env: "staging", typ: "staging", state: "successful", n: 301, secs: 169, at: ago(108)},
				{env: "production", typ: "production", state: "successful", n: 302, secs: 251, at: ago(100)}}}
	case "DEMO-13":
		return &devWork{tool: "GitHub", repo: "shop-web", branch: "issue/DEMO-13-house-number",
			prs: []devPR{{n: 421, title: "DEMO-13 Keep the house number in the address form", status: "OPEN", src: "issue/DEMO-13-house-number", dst: "main",
				author: me, reviewers: []user{tomas}, comments: 0, updated: ago(6)}},
			commits: []devCommit{{"Reproduce: autofill drops the number", me, ago(30)}, {"Separate house-number input", me, ago(7)}},
			builds:  []devBuild{{n: 233, state: "successful", ref: "issue/DEMO-13-house-number", passed: 207, at: ago(6.5)}},
			deploys: []devDeploy{{env: "preview-421", typ: "testing", state: "successful", n: 88, secs: 61, at: ago(6.2)}}}
	}
	return nil
}

func (w *devWork) repoURL() string { return "https://code.example.com/acme/" + w.repo }

// devSummary is the Development field as Jira sends it, nil for none.
func (s *Server) devSummary(key string) any {
	w := s.devWork(key)
	if w == nil {
		return nil
	}
	state := ""
	for _, st := range []string{"OPEN", "MERGED", "DECLINED"} {
		for _, p := range w.prs {
			if state == "" && p.status == st {
				state = st
			}
		}
	}
	var top []map[string]string
	for _, typ := range []string{"production", "staging", "testing"} {
		for _, d := range w.deploys {
			if d.typ == typ && len(top) == 0 {
				top = append(top, map[string]string{"title": d.env})
			}
		}
	}
	cached, _ := json.Marshal(map[string]any{"cachedValue": map[string]any{"errors": []any{}, "summary": map[string]any{
		"pullrequest":            map[string]any{"overall": map[string]any{"count": len(w.prs), "state": state}},
		"deployment-environment": map[string]any{"overall": map[string]any{"count": len(w.deploys), "topEnvironments": top}},
	}}, "isStale": false})
	return fmt.Sprintf("{pullrequest={dataType=pullrequest, state=%s, stateCount=%d}, json=%s}", state, len(w.prs), cached)
}

// devStatus answers /rest/dev-status/latest/issue/{summary,detail}.
func (s *Server) devStatus(p string, get func(string) string) any {
	w := s.devWork(project + "-" + get("issueId"))
	if strings.HasSuffix(p, "/summary") {
		sum := map[string]any{}
		if w != nil {
			count := func(tool string, n int) map[string]any {
				return map[string]any{"byInstanceType": map[string]any{tool: map[string]any{"count": n, "name": tool}}}
			}
			sum["pullrequest"], sum["branch"], sum["repository"] = count(w.tool, len(w.prs)), count(w.tool, 1), count(w.tool, 1)
			sum["build"], sum["deployment-environment"] = count(ciTool, len(w.builds)), count(ciTool, len(w.deploys))
		}
		return map[string]any{"summary": sum}
	}
	detail := map[string]any{}
	tool, dataType := get("applicationType"), get("dataType")
	if w == nil || (tool != w.tool && tool != ciTool) {
		return map[string]any{"detail": []any{detail}}
	}
	person := func(u user) map[string]any { return map[string]any{"name": u.name} }
	commit := func(c devCommit, i int) map[string]any {
		id := fmt.Sprintf("%x", fakeHash(w.repo+c.msg))
		return map[string]any{"id": id, "displayId": id[:7], "message": c.msg, "author": person(c.author),
			"authorTimestamp": stamp(c.at), "url": w.repoURL() + "/commit/" + id, "fileCount": 1 + i%4}
	}
	switch dataType {
	case "pullrequest":
		var prs []any
		for _, p := range w.prs {
			var revs []any
			for i, r := range p.reviewers {
				revs = append(revs, map[string]any{"name": r.name, "approved": i < len(p.approved) && p.approved[i]})
			}
			prs = append(prs, map[string]any{"id": fmt.Sprint(p.n), "name": p.title, "status": p.status, "url": w.prURL(p.n),
				"source": map[string]any{"branch": p.src}, "destination": map[string]any{"branch": p.dst},
				"repositoryName": w.repo, "repositoryUrl": w.repoURL(), "author": person(p.author), "reviewers": revs,
				"commentCount": p.comments, "lastUpdate": stamp(p.updated)})
		}
		detail["pullRequests"] = prs
	case "branch":
		last := commit(w.commits[len(w.commits)-1], len(w.commits)-1)
		detail["branches"] = []any{map[string]any{"name": w.branch, "url": w.repoURL() + "/tree/" + w.branch,
			"createPullRequestUrl": w.repoURL() + "/compare/" + w.branch, "repository": map[string]any{"name": w.repo, "url": w.repoURL()}, "lastCommit": last}}
	case "repository":
		var cs []any
		for i := len(w.commits) - 1; i >= 0; i-- { // newest first, as Jira lists them
			cs = append(cs, commit(w.commits[i], i))
		}
		detail["repositories"] = []any{map[string]any{"name": w.repo, "url": w.repoURL(), "commits": cs}}
	case "build":
		var bs []any
		for _, b := range w.builds {
			bs = append(bs, map[string]any{"name": "CI", "displayName": w.repo + " CI", "buildNumber": b.n, "state": b.state,
				"url": w.repoURL() + "/pipelines/" + fmt.Sprint(b.n), "lastUpdated": stamp(b.at),
				"testSummary": map[string]any{"totalNumber": b.passed + b.failed + b.skipped, "numberPassed": b.passed, "numberFailed": b.failed, "numberSkipped": b.skipped},
				"references":  []any{map[string]any{"ref": map[string]any{"name": b.ref}}}})
		}
		detail["builds"] = bs
	case "deployment-environment":
		var ds []any
		for _, d := range w.deploys {
			ds = append(ds, map[string]any{"displayName": fmt.Sprintf("Deploy #%d", d.n), "state": d.state, "duration": d.secs,
				"url": w.repoURL() + "/deployments/" + fmt.Sprint(d.n), "lastUpdated": stamp(d.at),
				"environment": map[string]any{"displayName": d.env, "type": d.typ}, "pipeline": map[string]any{"displayName": w.repo + " deploy"}})
		}
		detail["deployments"] = ds
	}
	return map[string]any{"detail": []any{detail}}
}

func (w *devWork) prURL(n int) string {
	if w.tool == "GitLab" {
		return w.repoURL() + "/-/merge_requests/" + fmt.Sprint(n)
	}
	return w.repoURL() + "/pull/" + fmt.Sprint(n)
}

// fakeHash is a stable made-up commit hash.
func fakeHash(s string) [20]byte {
	var h [20]byte
	x := uint32(2166136261)
	for i := range h {
		for _, c := range s {
			x = (x ^ uint32(c) ^ uint32(i)) * 16777619
		}
		h[i] = byte(x >> 8)
	}
	return h
}
