package application

import (
	"context"
	"errors"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

type displaySettingsFake struct {
	value  settings.Display
	err    error
	saves  int
	mode   settings.WorkListMode
	order  settings.WorkSort
	future bool
}

func (f *displaySettingsFake) ShowFutureSchedules(context.Context) (bool, error) {
	return f.future, f.err
}
func (f *displaySettingsFake) SetShowFutureSchedules(_ context.Context, visible bool) error {
	f.saves++
	if f.err != nil {
		return f.err
	}
	f.future = visible
	return nil
}

func (f *displaySettingsFake) DisplaySettings(context.Context) (settings.Display, error) {
	return f.value, f.err
}

func (f *displaySettingsFake) WorkSort(context.Context) (settings.WorkSort, error) {
	return f.order, f.err
}
func (f *displaySettingsFake) SetWorkSort(_ context.Context, order settings.WorkSort) error {
	f.saves++
	if f.err != nil {
		return f.err
	}
	f.order = order
	return nil
}
func (f *displaySettingsFake) WorkListMode(context.Context) (settings.WorkListMode, error) {
	return f.mode, f.err
}
func (f *displaySettingsFake) SetWorkListMode(_ context.Context, mode settings.WorkListMode) error {
	f.saves++
	if f.err != nil {
		return f.err
	}
	f.mode = mode
	return nil
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

func TestWorkListModeDelegatesAndRejectsBeforeSave(t *testing.T) {
	ctx := context.Background()
	repo := &displaySettingsFake{mode: settings.Cards}
	app := NewDisplaySettings(repo)
	if mode, err := app.LoadWorkListMode(ctx); err != nil || mode != settings.Cards || repo.saves != 0 {
		t.Fatal(mode, err)
	}
	if err := app.SetWorkListMode(ctx, settings.WorkListMode("invalid")); !errors.Is(err, settings.ErrWorkListMode) || repo.saves != 0 {
		t.Fatal(err)
	}
	if err := app.SetWorkListMode(ctx, settings.Rows); err != nil || repo.saves != 1 || repo.mode != settings.Rows {
		t.Fatal(err)
	}
	repo.err = errors.New("failure")
	if _, err := app.LoadWorkListMode(ctx); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	if err := app.SetWorkListMode(ctx, settings.Cards); !errors.Is(err, repo.err) || repo.mode != settings.Rows {
		t.Fatal(err)
	}
}

func TestWorkSortDelegatesAndRejectsBeforeSave(t *testing.T) {
	ctx := context.Background()
	repo := &displaySettingsFake{order: settings.RegistrationAsc}
	app := NewDisplaySettings(repo)
	if got, err := app.LoadWorkSort(ctx); err != nil || got != settings.RegistrationAsc || repo.saves != 0 {
		t.Fatal(got, err)
	}
	if err := app.SetWorkSort(ctx, settings.WorkSort("invalid")); !errors.Is(err, settings.ErrWorkSort) || repo.saves != 0 {
		t.Fatal(err)
	}
	if err := app.SetWorkSort(ctx, settings.TitleAsc); err != nil || repo.saves != 1 || repo.order != settings.TitleAsc {
		t.Fatal(err)
	}
	repo.err = errors.New("failure")
	if _, err := app.LoadWorkSort(ctx); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	if err := app.SetWorkSort(ctx, settings.TitleDesc); !errors.Is(err, repo.err) || repo.order != settings.TitleAsc {
		t.Fatal(err)
	}
}

func TestFutureSchedulesDelegatesWithoutImplicitSave(t *testing.T) {
	ctx := context.Background()
	repo := &displaySettingsFake{future: true}
	app := NewDisplaySettings(repo)
	if got, err := app.LoadShowFutureSchedules(ctx); err != nil || !got || repo.saves != 0 {
		t.Fatal(got, err, repo.saves)
	}
	for _, visible := range []bool{false, true} {
		if err := app.SetShowFutureSchedules(ctx, visible); err != nil {
			t.Fatal(err)
		}
		if got, err := app.LoadShowFutureSchedules(ctx); err != nil || got != visible {
			t.Fatal(got, err)
		}
	}
	repo.err = context.Canceled
	if _, err := app.LoadShowFutureSchedules(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := app.SetShowFutureSchedules(ctx, false); !errors.Is(err, context.Canceled) || !repo.future {
		t.Fatal(err)
	}
}
