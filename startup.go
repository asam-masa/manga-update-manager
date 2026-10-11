package main

import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

// runApplication gates every production DB/file operation before Wails starts.
// App's stopped state rejects delayed Wails startup callbacks after shutdown.
func runApplication(app *App, acquire func(string) (io.Closer, error), run func(*App) error) error {
	directory, err := app.dataDirectory()
	if err != nil {
		return err
	}
	lock, err := acquire(directory)
	if err != nil {
		return err
	}
	defer func() {
		// Wails also invokes shutdown. Repeat it safely on early run failures,
		// and keep the storage lock until the connection has been closed.
		app.shutdown(context.Background())
		if err := lock.Close(); err != nil {
			log.Print("保存先の排他を解放できませんでした。アプリ終了時に解放されます。")
		}
	}()
	// Resolve once so the lock and database always use the same directory.
	app.dataDirectory = func() (string, error) { return directory, nil }
	return run(app)
}

func startupErrorMessage(err error) string {
	if errors.Is(err, platform.ErrStorageInUse) {
		return "既に起動しています。開いているアプリをご利用ください。"
	}
	return "安全に起動できませんでした。保存先の空き容量とアクセス権限を確認し、アプリを終了して再度お試しください。"
}
