package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// place is where the window was last: its bounds outside maximised, and
// whether it was maximised. Moved is whether X and Y were ever saved.
type place struct {
	X, Y, W, H int
	Max, Moved bool
}

func placePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "laneway", "desktop-window.json")
}

// loadPlace is the last place, or a 1280×820 window centred.
func loadPlace() place {
	p := place{W: 1280, H: 820}
	if b, err := os.ReadFile(placePath()); err == nil {
		var saved place
		if json.Unmarshal(b, &saved) == nil && saved.W >= 480 && saved.H >= 360 {
			p = saved
		}
	}
	return p
}

// restore moves win to p when p is on a screen still there; else the OS
// places it. Wayland ignores positions anyway.
func restore(app *application.App, win *application.WebviewWindow, p place) {
	if !p.Moved {
		return
	}
	for _, s := range app.Screen.GetAll() {
		b := s.Bounds
		if p.X >= b.X && p.X < b.X+b.Width-100 && p.Y >= b.Y && p.Y < b.Y+b.Height-100 {
			win.SetPosition(p.X, p.Y)
			return
		}
	}
}

// remember saves win's place a moment after it moves or resizes.
func remember(win *application.WebviewWindow, p place) {
	var mu sync.Mutex // guards timer
	var timer *time.Timer
	save := func() {
		if win.IsFullscreen() || win.IsMinimised() {
			return
		}
		if p.Max = win.IsMaximised(); !p.Max {
			p.X, p.Y = win.Position()
			p.W, p.H = win.Size()
			p.Moved = true
		}
		if b, err := json.Marshal(p); err == nil && p.W > 0 {
			_ = os.MkdirAll(filepath.Dir(placePath()), 0o700)
			_ = os.WriteFile(placePath(), b, 0o600)
		}
	}
	later := func(*application.WindowEvent) {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(500*time.Millisecond, save)
	}
	for _, e := range []events.WindowEventType{events.Common.WindowDidMove, events.Common.WindowDidResize, events.Common.WindowMaximise, events.Common.WindowUnMaximise} {
		win.OnWindowEvent(e, later)
	}
}
