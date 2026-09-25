package jira

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDateTime reads a moment as typed: a day as ParseDate takes it,
// optionally followed by a clock time ("fri 14:00", "2026-10-01 9:30");
// without one it is 09:00.
func ParseDateTime(s string, now time.Time) (time.Time, error) {
	f := strings.Fields(s)
	clock := "09:00"
	if n := len(f); n >= 2 && strings.Contains(f[n-1], ":") {
		clock, f = f[n-1], f[:n-1]
	}
	d, err := ParseDate(strings.Join(f, " "), now)
	if err != nil {
		return time.Time{}, err
	}
	c, err := time.Parse("15:04", clock)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a time (14:00)", clock)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), c.Hour(), c.Minute(), 0, 0, d.Location()), nil
}

// ParseDate reads a day as typed: 2006-01-02, today, tomorrow, yesterday,
// +3d / -1w / +2m (days, weeks, months from now), or a weekday name or its
// first three letters for the next such day.
func ParseDate(s string, now time.Time) (time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	case "yesterday":
		return today.AddDate(0, 0, -1), nil
	}
	if d, err := time.ParseInLocation(time.DateOnly, s, now.Location()); err == nil {
		return d, nil
	}
	if len(s) >= 3 && (s[0] == '+' || s[0] == '-') {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err == nil {
			switch s[len(s)-1] {
			case 'd':
				return today.AddDate(0, 0, n), nil
			case 'w':
				return today.AddDate(0, 0, 7*n), nil
			case 'm':
				return today.AddDate(0, n, 0), nil
			}
		}
	}
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		name := strings.ToLower(wd.String())
		if len(s) >= 3 && strings.HasPrefix(name, s) {
			days := (int(wd) - int(today.Weekday()) + 7) % 7
			if days == 0 {
				days = 7
			}
			return today.AddDate(0, 0, days), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date (2006-01-02, today, +3d, fri)", s)
}
