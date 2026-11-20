//go:build !darwin

package main

import (
	_ "embed"

	"govault/internal/appcore"

	"github.com/getlantern/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

func runTray(app *appcore.App) {
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("TheVault")
		systray.SetTooltip("TheVault")

		showItem := systray.AddMenuItem("Show TheVault", "Show the main window")
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("Quit", "Quit TheVault")

		go func() {
			for {
				select {
				case <-showItem.ClickedCh:
					app.ShowWindow()
				case <-quitItem.ClickedCh:
					app.Shutdown()
				}
			}
		}()
	}, func() {
		// Nothing extra to do — appcore.App.OnShutdown (wired as Wails' own
		// OnShutdown hook in runWails) already handles stopping mining/DB/etc.
	})
}
