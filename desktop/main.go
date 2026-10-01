// Laneway desktop: a window around `laneway web`, with native notifications.
// The laneway binary next to this one (or on PATH) serves on loopback; this
// app only shows it. Its own module, so the rest of laneway stays CGO-free.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

const addr = "127.0.0.1:8484"

// notifyJS replaces the page's Notification with one posting to this app.
//
//go:embed notify.js
var notifyJS string

func main() {
	ns := notifications.New()
	var win *application.WebviewWindow
	origin := "http://" + addr

	app := application.New(application.Options{
		Name:     "Laneway",
		Services: []application.Service{application.NewService(ns)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		RawMessageHandler: func(_ application.Window, msg string, o *application.OriginInfo) {
			if o == nil || !o.IsMainFrame || strings.TrimSuffix(o.Origin, "/") != origin {
				return // only the laneway page may post
			}
			handle(ns, win, msg)
		},
	})

	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Laneway",
		Width:  1280,
		Height: 820,
		HTML:   `<body style="font:14px system-ui;color:#888;display:grid;place-items:center;height:90vh">Starting laneway…</body>`,
		JS:     notifyJS,
	})

	if runtime.GOOS == "darwin" { // a Mac app stays running with its window closed: the dock brings it back
		win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			win.Hide()
			e.Cancel()
		})
		app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
			win.Show()
		})
	}

	ns.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		win.Show()
		win.Focus()
		if id, ok := r.Response.UserInfo["id"].(string); ok {
			win.ExecJS(fmt.Sprintf("window.__lanewayDesktop?.click(%q)", id))
		}
	})

	srv := &server{addr: addr}
	app.OnShutdown(srv.stop)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			if err := srv.start(); err != nil {
				log.Print(err)
				app.Dialog.Error().SetTitle("Laneway did not start").SetMessage(err.Error()).Show()
				app.Quit()
				return
			}
			win.SetURL(origin)
		}()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// handle is a message from notify.js: check or ask for permission, or show one.
func handle(ns *notifications.NotificationService, win *application.WebviewWindow, msg string) {
	var m struct{ Type, ID, Title, Body, Tag string }
	if json.Unmarshal([]byte(msg), &m) != nil {
		return
	}
	answer := func(p string) { win.ExecJS(fmt.Sprintf("window.__lanewayDesktop?.permission(%q)", p)) }
	switch m.Type {
	case "check":
		if ok, err := ns.CheckNotificationAuthorization(); err == nil && ok {
			answer("granted")
		}
	case "permission":
		go func() { // waits on the user
			ok, err := ns.RequestNotificationAuthorization()
			if err != nil {
				log.Print("notifications: ", err)
			}
			answer(map[bool]string{true: "granted", false: "denied"}[ok && err == nil])
		}()
	case "notify":
		id := m.Tag
		if id == "" {
			id = m.ID
		}
		err := ns.SendNotification(notifications.NotificationOptions{ID: id, Title: m.Title, Body: m.Body, Data: map[string]any{"id": m.ID}})
		if err != nil {
			log.Print("notifications: ", err)
		}
	}
}
