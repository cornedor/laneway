package jira

import (
	"context"
	"fmt"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestZZProbe(t *testing.T) {
	raw, _ := os.ReadFile(os.Getenv("HOME") + "/.config/laneway/config.yaml")
	var cfg struct {
		Jira struct {
			BaseURL  string `yaml:"base_url"`
			Email    string `yaml:"email"`
			APIToken string `yaml:"api_token"`
		} `yaml:"jira"`
	}
	yaml.Unmarshal(raw, &cfg)
	c := New(Config{BaseURL: cfg.Jira.BaseURL, Email: cfg.Jira.Email, APIToken: cfg.Jira.APIToken})
	for _, b := range []int{101, 83, 102} {
		body, err := c.doRaw(context.Background(), "GET", fmt.Sprintf("/rest/greenhopper/1.0/cardcolors/%d/strategy/custom", b), "x", nil)
		s := string(body)
		if false {
			s = s[:300]
		}
		fmt.Println(b, err, s)
		cc, err := c.CardColors(context.Background(), b)
		fmt.Printf("%d parsed: %+v %v\n", b, cc, err)
	}
}
