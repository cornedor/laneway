package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/forge"
)

// TestJobPicker: p lists the pipeline's jobs, the ones that want a look
// first; enter on one opens its log.
func TestJobPicker(t *testing.T) {
	m := loadedJiraModel(t)
	m.mr = &panelMR{ref: forge.Ref{Repo: "g/p", Number: 7}, mr: &forge.Change{Title: "T", Checks: &forge.Checks{Status: forge.StatusRunning, Groups: []forge.Group{
		{Name: "test", Jobs: []forge.Job{{ID: 1, Name: "unit", Status: forge.StatusSuccess}, {ID: 2, Name: "lint", Status: forge.StatusFailed}}},
		{Name: "deploy", Jobs: []forge.Job{{ID: 3, Name: "staging", Status: forge.StatusRunning}}},
	}}}}
	out, _, ok := m.mrKey(keyMsg(t, "p"))
	if m = out.(Model); !ok || !m.jiraPicker.active || m.jiraPicker.kind != jiraPickJob {
		t.Fatal("p: no job picker")
	}
	var ids []string
	for _, it := range m.jiraPicker.items {
		ids = append(ids, it.id)
	}
	if strings.Join(ids, ",") != "2,3,1" {
		t.Errorf("order %v, want the failed and running before the passed", ids)
	}
	out, cmd := m.applyJiraPick()
	if m = out.(Model); m.jobLog == nil || m.jobLog.id != 2 || m.jobLog.name != "lint" || cmd == nil {
		t.Fatalf("enter: job log %+v", m.jobLog)
	}
}

// TestJobLogFollows: a running job's log keeps its end in sight and is read
// again; scrolling up stops following, G follows again; a finished one is
// read no more.
func TestJobLogFollows(t *testing.T) {
	m := loadedJiraModel(t)
	m.jobLog = &jobLogState{id: 3, name: "staging", gen: 1, follow: true, viewH: 5}
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i+1)
	}
	out, cmd := m.handleJobLog(jobLogMsg{gen: 1, job: &forge.JobLog{Job: forge.Job{Status: forge.StatusRunning}}, lines: lines})
	m = out.(Model)
	if m.jobLog.top != 15 || cmd == nil {
		t.Fatalf("running: top %d, cmd %v", m.jobLog.top, cmd != nil)
	}
	out, _ = m.handleJobLogKey(keyMsg(t, "up"))
	if m = out.(Model); m.jobLog.follow || m.jobLog.top != 14 {
		t.Errorf("up: follow %v, top %d", m.jobLog.follow, m.jobLog.top)
	}
	out, _ = m.handleJobLog(jobLogMsg{gen: 1, job: &forge.JobLog{Job: forge.Job{Status: forge.StatusRunning}}, lines: append(lines, "more")})
	if m = out.(Model); m.jobLog.top != 14 {
		t.Errorf("not following: the view moved to %d", m.jobLog.top)
	}
	out, _ = m.handleJobLogKey(keyMsg(t, "G"))
	if m = out.(Model); !m.jobLog.follow || m.jobLog.top != 16 {
		t.Errorf("G: follow %v, top %d", m.jobLog.follow, m.jobLog.top)
	}
	if _, cmd = m.handleJobLog(jobLogMsg{gen: 1, job: &forge.JobLog{Job: forge.Job{Status: forge.StatusSuccess}}, lines: lines}); cmd != nil {
		t.Error("a finished job read again")
	}
	m.width = 100
	if v := m.renderJobLog(12); !strings.Contains(v, "20  line 20") {
		t.Errorf("render:\n%s", v)
	}
	out, _ = m.handleJobLogKey(keyMsg(t, "esc"))
	if out.(Model).jobLog != nil {
		t.Error("esc kept the log")
	}
}
