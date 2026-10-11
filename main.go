package main

import (
	"embed"
	"os"

	"github.com/asam-masa/manga-update-manager/internal/platform"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := runApplication(NewApp(), platform.AcquireStorageLock, runDesktop); err != nil {
		message := startupErrorMessage(err)
		if noticeErr := platform.ShowStartupError(message); noticeErr != nil {
			// Do not expose paths or raw OS diagnostics, even if the dialog fails.
			println(message)
		}
		os.Exit(1)
	}
}

func runDesktop(app *App) error {
	return wails.Run(&options.App{
		Title:  "Manga Update Manager",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 249, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		ErrorFormatter:   formatAPIError,
		Bind: []interface{}{
			app,
		},
	})
}
