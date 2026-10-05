package standup

import (
	"fmt"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/config"
)

// Settings are the ui.standup_* options.
type Settings struct {
	First    bool          // open on the first person, not everyone
	Lookback int           // workdays back the activity starts
	Length   time.Duration // the whole standup, split over the people
	Timebox  time.Duration // each person's turn instead, 0 for the split
	Shuffle  bool          // the people in a random order, not the board's
}

// Defaults: everyone first, since the previous workday, 15 minutes.
var Defaults = Settings{Lookback: 1, Length: 15 * time.Minute}

// Parse reads c's standup options over Defaults, warning of bad ones.
func Parse(c config.UIConfig) (Settings, []string) {
	s, warn := Defaults, []string(nil)
	switch v := strings.TrimSpace(c.StandupStart); v {
	case "", "everyone":
	case "first":
		s.First = true
	default:
		warn = append(warn, fmt.Sprintf("ui.standup_start: %q is not everyone or first", v))
	}
	switch n := c.StandupLookback; {
	case n == 0:
	case n < 1 || n > 10:
		warn = append(warn, fmt.Sprintf("ui.standup_lookback: %d is not 1–10", n))
	default:
		s.Lookback = n
	}
	dur := func(name, v string, dst *time.Duration) {
		if v = strings.TrimSpace(v); v == "" {
			return
		}
		d, err := time.ParseDuration(v)
		if err != nil || d < 10*time.Second || d > 2*time.Hour {
			warn = append(warn, fmt.Sprintf("ui.%s: %q is not a duration from 10s to 2h", name, v))
			return
		}
		*dst = d
	}
	dur("standup_length", c.StandupLength, &s.Length)
	dur("standup_timebox", c.StandupTimebox, &s.Timebox)
	switch v := strings.TrimSpace(c.StandupShuffle); v {
	case "", "off":
	case "on":
		s.Shuffle = true
	default:
		warn = append(warn, fmt.Sprintf("ui.standup_shuffle: %q is not on or off", v))
	}
	return s, warn
}

// Turn is each person's time: the timebox, else the length split over
// people.
func (s Settings) Turn(people int) time.Duration {
	if s.Timebox > 0 || people == 0 {
		return s.Timebox
	}
	return (s.Length / time.Duration(people)).Truncate(time.Second)
}
