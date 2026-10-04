package main

import "testing"

func TestAppStatus(t *testing.T) {
	app := NewApp()

	if got := app.Status(); got != "ready" {
		t.Fatalf("Status() = %q, want %q", got, "ready")
	}
}
