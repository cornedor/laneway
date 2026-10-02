package main

import (
	"fmt"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

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
