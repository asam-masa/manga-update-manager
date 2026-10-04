package main

// App exposes the application boundary to the Wails frontend.
type App struct{}

// NewApp creates the Wails application boundary.
func NewApp() *App {
	return &App{}
}

// Status confirms that the frontend can call the Go boundary.
func (a *App) Status() string {
	return "ready"
}
