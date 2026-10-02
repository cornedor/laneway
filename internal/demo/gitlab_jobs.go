package demo

import (
	"fmt"
	"strings"
	"time"
)

// !87's pipeline (#5521) as the demo's GitLab runs it: test and build done,
// deploy:staging still running for deployRun after the pipeline is first
// read, its log growing a line every deployStep, so a job's log view that
// follows a running job has something to follow. Logs are GitLab's: ANSI
// colours and section markers.

const (
	deployRun  = 36 * time.Second
	deployStep = 3 * time.Second
)

type demoJob struct {
	id                  int
	name, stage, status string
	allowFailure        bool
	duration            int
	log                 string
}

// deployElapsed is how long deploy:staging has run, starting it on the
// first read.
func (s *Server) deployElapsed() time.Duration {
	if s.git.deployAt.IsZero() {
		s.git.deployAt = time.Now()
	}
	return time.Since(s.git.deployAt)
}

// pipelineRunning is whether #5521 is still deploying.
func (s *Server) pipelineRunning() bool { return s.deployElapsed() < deployRun }

const ansiReset = "\x1b[0;m"

// jobLog is a job's log: the runner's preamble, its script's lines, and how
// it ended ("" while it runs).
func jobLog(image, script string, body []string, end string) string {
	sec := func(name, title string) string {
		return "section_start:1727853000:" + name + "\r\x1b[0K\x1b[0K\x1b[36;1m" + title + ansiReset
	}
	secEnd := func(name string) string { return "section_end:1727853060:" + name + "\r\x1b[0K" }
	lines := []string{
		"\x1b[0KRunning with gitlab-runner 17.4.0 (b92ee590)" + ansiReset,
		"\x1b[0K  on shared-runner-3 Xy1zAb9q, system ID: s_8d2c41f0" + ansiReset,
		sec("prepare_executor", "Preparing the \"docker\" executor"),
		"\x1b[0KUsing Docker executor with image " + image + " ..." + ansiReset,
		"\x1b[0KPulling docker image " + image + " ...  10%\r\x1b[0KPulling docker image " + image + " ...  64%\r\x1b[0KPulling docker image " + image + " ... done" + ansiReset,
		secEnd("prepare_executor"),
		sec("get_sources", "Getting source from Git repository"),
		"\x1b[32;1mFetching changes with git depth set to 20..." + ansiReset,
		"Checking out f3428a1d as detached HEAD (ref is issue/DEMO-7-shipping-rate-cache)...",
		secEnd("get_sources"),
		sec("step_script", "Executing \"step_script\" stage of the job script"),
		"\x1b[32;1m$ " + script + ansiReset,
	}
	lines = append(lines, body...)
	if end != "" {
		lines = append(lines, secEnd("step_script"), end)
	}
	return strings.Join(lines, "\n") + "\n"
}

const (
	jobOK = "\x1b[32;1mJob succeeded" + ansiReset
	jobKO = "\x1b[31;1mERROR: Job failed: exit code 1" + ansiReset
)

// gitlabJobs are #5521's jobs, oldest first.
func (s *Server) gitlabJobs() []demoJob {
	el := s.deployElapsed()
	deploy := []string{
		"\x1b[32;1m$ helm upgrade --install shop-api ./chart --namespace staging --wait" + ansiReset,
		"Release \"shop-api\" has been upgraded. Happy Helming!",
		"Waiting for deployment \"shop-api\" rollout to finish: 0 of 3 updated replicas are available...",
		"Waiting for deployment \"shop-api\" rollout to finish: 1 of 3 updated replicas are available...",
		"Waiting for deployment \"shop-api\" rollout to finish: 2 of 3 updated replicas are available...",
		"deployment \"shop-api\" successfully rolled out",
		"\x1b[32;1m$ ./scripts/smoke.sh https://staging.shop.example" + ansiReset,
		"GET /healthz \x1b[32m200\x1b[0m 12ms",
		"GET /api/shipping/rates?region=eu \x1b[32m200\x1b[0m 41ms \x1b[33m(cache miss)\x1b[0m",
		"GET /api/shipping/rates?region=eu \x1b[32m200\x1b[0m 3ms \x1b[32m(cache hit)\x1b[0m",
		"GET /api/shipping/rates?region=us \x1b[32m200\x1b[0m 38ms \x1b[33m(cache miss)\x1b[0m",
		"\x1b[1msmoke: 4/4 passed\x1b[0m",
	}
	status, end, dur := "success", jobOK, int(deployRun/time.Second)
	if el < deployRun {
		deploy = deploy[:min(int(el/deployStep)+1, len(deploy))]
		status, end, dur = "running", "", int(el/time.Second)
	}
	tests := []string{
		"ok  \tacme/shop-api/internal/cart\t0.412s",
		"ok  \tacme/shop-api/internal/checkout\t1.208s",
		"ok  \tacme/shop-api/internal/shipping\t0.377s\tcoverage: 84.1% of statements",
		"?   \tacme/shop-api/cmd/shop-api\t[no test files]",
	}
	load := []string{
		"\x1b[1mscenario: checkout_with_rates\x1b[0m  vus=50 duration=60s",
		"  http_req_duration..........: avg=88ms  p(95)=\x1b[31m612ms\x1b[0m",
		"  http_req_failed............: 0.41%",
		"\x1b[31m✗ p(95)<500ms\x1b[0m threshold crossed: carrier sandbox slow at the cut-off",
	}
	return []demoJob{
		{id: 90311, name: "go test", stage: "test", status: "success", duration: 74, log: jobLog("golang:1.25", "go test -race ./...", tests, jobOK)},
		{id: 90312, name: "lint", stage: "test", status: "success", duration: 31, log: jobLog("golangci/golangci-lint:v2", "golangci-lint run", []string{"0 issues."}, jobOK)},
		{id: 90313, name: "load-test", stage: "test", status: "failed", allowFailure: true, duration: 96, log: jobLog("grafana/k6:0.53", "k6 run load/checkout.js", load, jobKO)},
		{id: 90314, name: "docker", stage: "build", status: "success", duration: 58, log: jobLog("docker:27", "docker build -t registry.example/acme/shop-api:f3428a1d .",
			[]string{"#8 [build 4/4] RUN go build -o /shop-api ./cmd/shop-api", "#8 DONE 21.4s", "#10 naming to registry.example/acme/shop-api:f3428a1d done"}, jobOK)},
		{id: 90315, name: "deploy:staging", stage: "deploy", status: status, duration: dur, log: jobLog("alpine/helm:3.16", "helm version --short", append([]string{"v3.16.2+g13654a5"}, deploy...), end)},
	}
}

// gitlabJob is #5521's job id, nil for none.
func (s *Server) gitlabJob(id int) *demoJob {
	for _, j := range s.gitlabJobs() {
		if j.id == id {
			return &j
		}
	}
	return nil
}

// json is the job as the API sends it.
func (j demoJob) json(repoURL string) map[string]any {
	return map[string]any{"id": j.id, "name": j.name, "stage": j.stage, "status": j.status, "allow_failure": j.allowFailure,
		"duration": j.duration, "web_url": fmt.Sprintf("%s/-/jobs/%d", repoURL, j.id)}
}
