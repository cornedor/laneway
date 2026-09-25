package ui

import (
	"fmt"
	"strings"
	"time"

	"jiratui/internal/config"
)

// options are the config's ui: section with defaults filled in.
type options struct {
	autoRefresh  time.Duration // 0: off
	staleAfter   time.Duration
	images       bool
	imageMaxRows int
	panelPct     int
}

func defaultOptions() options {
	return options{autoRefresh: 2 * time.Minute, staleAfter: time.Minute, images: true, imageMaxRows: 16, panelPct: 50}
}

// optionsFrom resolves c over the defaults. A bad value is reported and
// the default kept, so a typo never stops the app.
func optionsFrom(c config.UIConfig) (options, []string) {
	o := defaultOptions()
	var warn []string
	dur := func(name, v string, dst *time.Duration, allowOff bool) {
		switch v = strings.TrimSpace(v); {
		case v == "":
		case allowOff && (v == "off" || v == "0"):
			*dst = 0
		default:
			d, err := time.ParseDuration(v)
			if err != nil || d < 5*time.Second {
				warn = append(warn, fmt.Sprintf("ui.%s: %q is not a duration of 5s or more", name, v))
				return
			}
			*dst = d
		}
	}
	dur("auto_refresh", c.AutoRefresh, &o.autoRefresh, true)
	dur("stale_after", c.StaleAfter, &o.staleAfter, false)
	switch strings.ToLower(strings.TrimSpace(c.Images)) {
	case "", "auto":
	case "off":
		o.images = false
	default:
		warn = append(warn, fmt.Sprintf("ui.images: %q is not auto or off", c.Images))
	}
	switch n := c.ImageMaxRows; {
	case n == 0:
	case n < 1 || n > 200:
		warn = append(warn, fmt.Sprintf("ui.image_max_rows: %d is not 1–200", n))
	default:
		o.imageMaxRows = n
	}
	switch n := c.PanelWidth; {
	case n == 0:
	case n < 20 || n > 80:
		warn = append(warn, fmt.Sprintf("ui.panel_width: %d is not 20–80", n))
	default:
		o.panelPct = n
	}
	return o, warn
}
