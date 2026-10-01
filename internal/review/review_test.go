package review

import (
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

func TestKeys(t *testing.T) {
	reqs := []Request{{Title: "DEMO-4 fix, see OTHER-1 and DEMO-4"}, {Branch: "issue/demo-5-x"}, {Title: "nothing"}, {Title: "utf-8 paths"}}
	if got := strings.Join(Keys(reqs, []jira.Project{{Key: "DEMO"}}), ","); got != "DEMO-4,DEMO-5" {
		t.Errorf("keys = %q", got)
	}
}

// TestReviewerMRsPath: a username can't add parameters to the query.
func TestReviewerMRsPath(t *testing.T) {
	if got := ReviewerMRsPath("a&scope=x#y"); !strings.HasSuffix(got, "&reviewer_username=a%26scope%3Dx%23y") {
		t.Errorf("path = %q", got)
	}
}
