package gitlab

import (
	"errors"
	"net/http"
	"os/exec"
	"strings"

	"github.com/cornedor/laneway/internal/forge"
)

// GlabInstall is where glab, GitLab's CLI, is installed from.
const GlabInstall = "https://gitlab.com/gitlab-org/cli#installation"

// SignIn is how to give laneway a token for Host: glab's login, or a
// personal access token under gitlab: in the config.
type SignIn struct {
	Host string
	// Glab is the command that signs in; Install where glab comes from,
	// when it is not on PATH.
	Glab    string
	Install string
	// TokenURL makes a personal access token with scope api.
	TokenURL string
	// Rejected is that the token was refused: expired or revoked.
	Rejected bool
}

// lookGlab finds glab on PATH; tests swap it.
var lookGlab = func() bool { _, err := exec.LookPath("glab"); return err == nil }

// SignInFor is how to sign in to host.
func SignInFor(host string) SignIn {
	si := SignIn{Host: host, Glab: "glab auth login --hostname " + host,
		TokenURL: "https://" + host + "/-/user_settings/personal_access_tokens?name=laneway&scopes=api"}
	if !lookGlab() {
		si.Install = GlabInstall
	}
	return si
}

// Lines are s in words, for the panel and the settings row.
func (s SignIn) Lines() []string {
	head := "No GitLab token for " + s.Host + "."
	if s.Rejected {
		head = s.Host + " rejected the token: expired or revoked."
	}
	glab := "Sign in with glab: " + s.Glab
	if s.Install != "" {
		glab += " (install glab: " + s.Install + ")"
	}
	return []string{head, glab,
		"Or add it under gitlab: in the config, base_url: https://" + s.Host + " with token (or token_cmd: [pass, gitlab]): " +
			"a personal access token with scope api (read_api only reads), made at " + s.TokenURL}
}

// IsMRLink is whether link is a GitLab merge request's, on any host.
func IsMRLink(link string) bool {
	return mrURLRe.MatchString(strings.TrimSpace(link)) && forge.HostOf(link) != ""
}

// Rejected is whether err is GitLab refusing the token (401).
func Rejected(err error) bool {
	var se *forge.StatusErr
	return errors.As(err, &se) && se.Code == http.StatusUnauthorized
}
