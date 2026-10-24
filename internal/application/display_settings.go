package application

import (
	"context"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

type DisplaySettingsRepository interface {
	DisplaySettings(context.Context) (settings.Display, error)
	SetRegistrationFormVisible(context.Context, bool) error
	WorkListMode(context.Context) (settings.WorkListMode, error)
	SetWorkListMode(context.Context, settings.WorkListMode) error
}

type DisplaySettings struct {
	repository DisplaySettingsRepository
}

func NewDisplaySettings(repository DisplaySettingsRepository) *DisplaySettings {
	return &DisplaySettings{repository: repository}
}

func (a *DisplaySettings) Load(ctx context.Context) (settings.Display, error) {
	return a.repository.DisplaySettings(ctx)
}

func (a *DisplaySettings) SetRegistrationFormVisible(ctx context.Context, visible bool) error {
	return a.repository.SetRegistrationFormVisible(ctx, visible)
}

func (a *DisplaySettings) LoadWorkListMode(ctx context.Context) (settings.WorkListMode, error) {
	return a.repository.WorkListMode(ctx)
}

func (a *DisplaySettings) SetWorkListMode(ctx context.Context, mode settings.WorkListMode) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	return a.repository.SetWorkListMode(ctx, mode)
}
