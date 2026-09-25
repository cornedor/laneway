package jira

import (
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 9, 25, 15, 4, 0, 0, time.UTC) // a Friday
	for in, want := range map[string]string{
		"2026-10-01": "2026-10-01",
		"today":      "2026-09-25",
		" Tomorrow ": "2026-09-26",
		"yesterday":  "2026-09-24",
		"+3d":        "2026-09-28",
		"-1w":        "2026-09-18",
		"+1m":        "2026-10-25",
		"mon":        "2026-09-28",
		"friday":     "2026-10-02",
		"sat":        "2026-09-26",
	} {
		got, err := ParseDate(in, now)
		if err != nil || got.Format(time.DateOnly) != want {
			t.Errorf("%q = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"soon", "+3x", "fr", "2026-13-01"} {
		if _, err := ParseDate(in, now); err == nil {
			t.Errorf("%q should fail", in)
		}
	}
}

func TestParseDateTime(t *testing.T) {
	now := time.Date(2026, 9, 25, 15, 4, 0, 0, time.UTC) // a Friday
	for in, want := range map[string]string{
		"2026-10-01 9:30": "2026-10-01 09:30",
		"mon 14:00":       "2026-09-28 14:00",
		"tomorrow":        "2026-09-26 09:00",
	} {
		got, err := ParseDateTime(in, now)
		if err != nil || got.Format("2006-01-02 15:04") != want {
			t.Errorf("%q = %v, %v; want %s", in, got, err, want)
		}
	}
	if _, err := ParseDateTime("fri 25:99", now); err == nil {
		t.Error("a bad clock should fail")
	}
}
