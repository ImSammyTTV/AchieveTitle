//go:build !darwin || cgo

package main

import (
	_ "embed"
	"os"
	"runtime"
	"time"

	"fyne.io/systray"
)

//go:embed assets/icon.ico
var trayICO []byte

//go:embed assets/tray.png
var trayPNG []byte

//go:embed assets/tray-template.png
var trayTemplatePNG []byte

// trayAvailable reports whether a tray / menu bar icon can be shown here.
func trayAvailable() bool {
	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" {
		// The Linux tray talks to the desktop over D-Bus; without a session
		// bus (servers, some minimal desktops) run without it.
		return os.Getenv("DBUS_SESSION_BUS_ADDRESS") != ""
	}
	return true
}

// runTray shows the tray icon and blocks until the app shuts down.
// It must be called from the main goroutine (macOS requirement).
func runTray(a *app) {
	go func() {
		a.wait()
		systray.Quit()
	}()
	systray.Run(func() { trayReady(a) }, func() {})
}

func trayReady(a *app) {
	switch runtime.GOOS {
	case "windows":
		systray.SetIcon(trayICO)
	case "darwin":
		systray.SetTemplateIcon(trayTemplatePNG, trayTemplatePNG)
	default:
		systray.SetIcon(trayPNG)
	}
	systray.SetTooltip("AchieveTitle")

	header := systray.AddMenuItem("AchieveTitle "+version, "")
	header.Disable()
	current := systray.AddMenuItem("Starting…", "Your current Twitch title")
	current.Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("Open settings", "Open the AchieveTitle settings page")
	auto := systray.AddMenuItemCheckbox("Update Twitch title automatically", "", a.store.Get().Enabled)
	update := systray.AddMenuItem("Update available…", "A new version of AchieveTitle is available")
	checkUpd := systray.AddMenuItem("Check for updates", "Look for a new version of AchieveTitle now")
	update.Hide()
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit AchieveTitle", "")

	refresh := func() {
		st := a.worker.Status()
		line := st.Title
		switch {
		case st.Error != "":
			line = "⚠ " + st.Error
		case line == "":
			line = "No title yet"
		}
		if r := []rune(line); len(r) > 60 {
			line = string(r[:59]) + "…"
		}
		current.SetTitle(line)
		systray.SetTooltip("AchieveTitle: " + line)
		if a.store.Get().Enabled {
			auto.Check()
		} else {
			auto.Uncheck()
		}
		if u := a.updater.Info(); u.Available {
			update.SetTitle("Update to " + u.Latest + "…")
			update.Show()
		} else {
			update.Hide()
		}
	}
	refresh()

	go func() {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-open.ClickedCh:
				openBrowser(a.srv.base)
			case <-update.ClickedCh:
				openBrowser(a.srv.base + "/#updates")
			case <-checkUpd.ClickedCh:
				checkUpd.SetTitle("Checking…")
				go func() {
					if err := a.updater.Check(a.ctx); err != nil {
						checkUpd.SetTitle("Couldn't check for updates")
					} else if a.updater.Info().Available {
						checkUpd.SetTitle("Check for updates")
						openBrowser(a.srv.base + "/#updates")
					} else {
						checkUpd.SetTitle("You're up to date (" + version + ")")
					}
					refresh()
					time.Sleep(5 * time.Second)
					checkUpd.SetTitle("Check for updates")
				}()
			case <-auto.ClickedCh:
				a.srv.setEnabledTo(!a.store.Get().Enabled)
				refresh()
			case <-quit.ClickedCh:
				a.stop()
				return
			case <-tick.C:
				refresh()
			}
		}
	}()
}
