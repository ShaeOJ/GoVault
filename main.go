package main

import (
	"context"
	"embed"

	"govault/internal/appcore"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// wailsHost is the desktop transport for appcore.App: Wails runtime IPC for
// events plus native window controls. The headless edge node supplies its own
// AppHost (HTTP + SSE) instead.
type wailsHost struct{ ctx context.Context }

func (w wailsHost) Emit(event string, data ...interface{}) { runtime.EventsEmit(w.ctx, event, data...) }
func (w wailsHost) Minimise()                              { runtime.WindowMinimise(w.ctx) }
func (w wailsHost) Show()                                  { runtime.WindowShow(w.ctx) }
func (w wailsHost) Unminimise()                            { runtime.WindowUnminimise(w.ctx) }
func (w wailsHost) Quit()                                  { runtime.Quit(w.ctx) }

func main() {
	app := appcore.NewApp()

	err := wails.Run(&options.App{
		Title:     "GoVault",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
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
