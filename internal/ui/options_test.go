package ui

import (
	"testing"
	"time"

	"jiratui/internal/config"
)

func TestOptionsFrom(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{})
	if o != defaultOptions() || len(warn) != 0 {
		t.Fatalf("empty config = %+v %v, want defaults", o, warn)
	}
	o, warn = optionsFrom(config.UIConfig{AutoRefresh: "off", StaleAfter: "30s", Images: "off", ImageMaxRows: 8, PanelWidth: 40})
	want := options{autoRefresh: 0, staleAfter: 30 * time.Second, images: false, imageMaxRows: 8, panelPct: 40}
	if o != want || len(warn) != 0 {
		t.Errorf("set = %+v %v, want %+v", o, warn, want)
	}
	o, warn = optionsFrom(config.UIConfig{AutoRefresh: "soon", StaleAfter: "1s", Images: "maybe", ImageMaxRows: -1, PanelWidth: 95})
	if o != defaultOptions() || len(warn) != 5 {
		t.Errorf("bad values = %+v, %d warnings %v; want defaults and 5", o, len(warn), warn)
	}
}

func TestAutoRefreshOff(t *testing.T) {
	m := jiraTabModel(t)
	m.opts.autoRefresh = 0
	if m.jiraAutoRefreshTick() != nil {
		t.Error("tick armed with auto_refresh off")
	}
}

func TestPanelWidthOption(t *testing.T) {
	if got := splitRightPane(200, 30); got != 60 {
		t.Errorf("30%% of 200 = %d", got)
	}
}
