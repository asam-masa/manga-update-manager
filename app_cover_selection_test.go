package main

import (
	"context"
	"testing"
)

func TestSelectWorkCoverOperationNotification(t *testing.T) {
	app := NewApp()
	app.dataDirectory = func() (string, error) { return t.TempDir(), nil }
	choices := 0
	app.chooseCover = func(context.Context) (string, error) { choices++; return "", nil }
	var received []string
	var receivedWork []int64
	app.coverSelectionFinished = func(_ context.Context, id int64, operation string) {
		received = append(received, operation)
		receivedWork = append(receivedWork, id)
	}
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	if _, err := app.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	work, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/notification", Title: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "secret/path", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "0000000000000000000000000000000g"} {
		if _, err := app.SelectWorkCoverWithOperation(work.ID, id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	if choices != 0 || len(received) != 0 {
		t.Fatal("invalid ID opened chooser or emitted event")
	}
	for _, id := range []string{"00000000000000000000000000000001", "00000000000000000000000000000002"} {
		result, err := app.SelectWorkCoverWithOperation(work.ID, id)
		if err != nil || !result.Cancelled || result.Work != nil {
			t.Fatal(result, err)
		}
	}
	if len(received) != 2 || received[0] != "00000000000000000000000000000001" || received[1] != "00000000000000000000000000000002" || receivedWork[0] != work.ID || receivedWork[1] != work.ID {
		t.Fatal(received, receivedWork)
	}
	if _, err := app.SelectWorkCover(work.ID); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 {
		t.Fatal("legacy API emitted notification")
	}
}
