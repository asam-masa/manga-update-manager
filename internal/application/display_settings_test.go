package application

import (
	"context"
	"errors"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

type displaySettingsFake struct {
	value settings.Display
	err   error
	saves int
}

func (f *displaySettingsFake) DisplaySettings(context.Context) (settings.Display, error) {
	return f.value, f.err
}
func (f *displaySettingsFake) SetRegistrationFormVisible(_ context.Context, visible bool) error {
	f.saves++
	if f.err != nil {
		return f.err
	}
	f.value.RegistrationFormVisible = visible
	return nil
}

func TestDisplaySettingsDelegatesWithoutImplicitSave(t *testing.T) {
	ctx := context.Background()
	repo := &displaySettingsFake{value: settings.DefaultDisplay()}
	app := NewDisplaySettings(repo)
	value, err := app.Load(ctx)
	if err != nil || !value.RegistrationFormVisible || repo.saves != 0 {
		t.Fatalf("load = %#v, %v, saves=%d", value, err, repo.saves)
	}
	if err := app.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if repo.value.RegistrationFormVisible || repo.saves != 1 {
		t.Fatal("requested visibility was not saved")
	}
	repo.err = errors.New("unavailable")
	if _, err := app.Load(ctx); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	if err := app.SetRegistrationFormVisible(ctx, true); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	if repo.value.RegistrationFormVisible {
		t.Fatal("failed save changed stored value")
	}
}
