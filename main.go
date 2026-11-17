package main

import (
	"context"
	"embed"
	"os"
	"path/filepath"

	"govault/internal/appcore"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// webviewDataDir keeps WebView2's user-data folder LOCAL to the executable
// (<exe>/data/webview2) instead of Wails' default %APPDATA%\<exeName>\EBWebView.
// The default path is fragile: it uses the exe's basename ("GoVault.exe") as a
// directory name, so a stray file of that name in %APPDATA% makes WebView2 fail
// with "We couldn't create the data directory" and the app never opens a window.
// Keeping it beside the exe (where we already store config/db) also makes the
// build portable. Returns "" on failure so Wails falls back to its default.
func webviewDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Join(filepath.Dir(exe), "data", "webview2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "" // not writable here — let Wails use its default
	}
	return dir
}

//go:embed all:frontend/dist
var assets embed.FS

// wailsHost is the desktop transport for appcore.App: Wails runtime IPC for
// events plus native window controls. The headless edge node supplies its own
// AppHost (HTTP + SSE) instead.
type wailsHost struct{ ctx context.Context }

func (w wailsHost) Emit(event string, data ...interface{}) { runtime.EventsEmit(w.ctx, event, data...) }
func (w wailsHost) Minimise()                              { runtime.WindowMinimise(w.ctx) }
func (w wailsHost) Hide()                                  { runtime.WindowHide(w.ctx) }
func (w wailsHost) Show()                                  { runtime.WindowShow(w.ctx) }
func (w wailsHost) Unminimise()                            { runtime.WindowUnminimise(w.ctx) }
func (w wailsHost) Quit()                                  { runtime.Quit(w.ctx) }

// main keeps wails.Run() on the real main goroutine, exactly as it ran before
// the tray was added. WebView2 is a single-threaded-apartment COM component:
// whatever OS thread creates the window must be the one continuously pumping
// its message loop. An earlier version of this file handed the main goroutine
// to systray.Run() instead and launched wails.Run() from a plain `go func`,
// which let Go's scheduler run the Wails/WebView2 window on a thread other
// than the one that created it — the window would then sit there with a valid
// handle and near-zero CPU but never answer window messages (Explorer reports
// "Not Responding") because nothing was pumping it anymore. systray.Run()
// instead gets its own background goroutine; it doesn't require the main
// thread on Windows the way Cocoa's tray does on macOS.
func main() {
	app := appcore.NewApp()
	go runTray(app)
	runWails(app)
}

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

func runWails(app *appcore.App) {
	err := wails.Run(&options.App{
		Title:     "TheVault",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 600,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Windows: &windows.Options{
			// Store WebView2 data beside the exe (portable) rather than in
			// %APPDATA%\GoVault.exe\EBWebView, which can't be created when a
			// stray file of that name exists. See webviewDataDir.
			WebviewUserDataPath: webviewDataDir(),
		},
		BackgroundColour: &options.RGBA{R: 10, G: 14, B: 26, A: 1},
		// Wire the Wails-backed host before the core starts up.
		OnStartup: func(ctx context.Context) {
			app.SetHost(wailsHost{ctx: ctx})
			app.OnStartup(ctx)
		},
		OnDomReady:    app.OnDomReady,
		OnShutdown:    app.OnShutdown,
		OnBeforeClose: app.OnBeforeClose,
		// Keep a single instance: relaunching the shortcut restores the window
		// that's running in the background instead of starting a second copy.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "govault-single-instance-a1f4",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				app.ShowWindow()
			},
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
