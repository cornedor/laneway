package main

import (
	"fmt"
	"maps"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// demoGitLab is the demo's GitLab, on its own server: glab's logins stay
// out of it.
func demoGitLab(baseURL string) *gitlab.Sites {
	return gitlab.NewSites([]gitlab.Config{{BaseURL: baseURL, Token: "demo"}}).WithoutGlab()
}

// gitlabRepos are every gitlab: instance's checkouts by project path.
func gitlabRepos(cfg config.Config) map[string]string {
	out := map[string]string{}
	for _, g := range cfg.GitLab {
		maps.Copy(out, g.Repos)
	}
	return out
}

// gitlabSites are the gitlab: instances with token_cmd run; one whose
// command fails warns and falls back to glab's login.
func gitlabSites(cfg config.Config) (*gitlab.Sites, []string) {
	var cfgs []gitlab.Config
	var warn []string
	for i, g := range cfg.GitLab {
		g, err := g.WithToken()
		if err != nil {
			warn = append(warn, fmt.Sprintf("gitlab[%d]: %v", i, err))
		}
		cfgs = append(cfgs, gitlab.Config{BaseURL: g.BaseURL, Token: g.Token})
	}
	return gitlab.NewSites(cfgs), warn
}
