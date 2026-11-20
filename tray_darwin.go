//go:build darwin

package main

import "govault/internal/appcore"

// getlantern/systray's darwin backend defines its own Cocoa AppDelegate
// class, which collides at link time with Wails' own AppDelegate
// ("duplicate symbol '_OBJC_METACLASS_$_AppDelegate'") — so the native tray
// icon is skipped on macOS; the main window works normally without it.
func runTray(app *appcore.App) {}
