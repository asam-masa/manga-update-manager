package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

// DisplaySettings reads without inserting or repairing persisted preferences.
func (s *Store) DisplaySettings(ctx context.Context) (settings.Display, error) {
	defaults := settings.DefaultDisplay()
	var visible int
	err := s.db.QueryRowContext(ctx, "SELECT registration_form_visible FROM display_settings WHERE id = 1").Scan(&visible)
	if errors.Is(err, sql.ErrNoRows) {
		return defaults, nil
	}
	if err != nil {
		return defaults, fmt.Errorf("read display settings: %w", err)
	}
	if visible != 0 && visible != 1 {
		return defaults, errors.New("invalid registration form visibility")
	}
	return settings.Display{RegistrationFormVisible: visible == 1}, nil
}

// SetRegistrationFormVisible updates only the explicitly requested preference.
func (s *Store) SetRegistrationFormVisible(ctx context.Context, visible bool) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO display_settings(id, registration_form_visible) VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET registration_form_visible = excluded.registration_form_visible`, visible)
	if err != nil {
		return fmt.Errorf("save display settings: %w", err)
	}
	return nil
}
