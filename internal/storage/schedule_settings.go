package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

// ShowFutureSchedules reads without creating or repairing a preference row.
func (s *Store) ShowFutureSchedules(ctx context.Context) (_ bool, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return true, err
	}
	defer finish(&err)
	var visible int
	err = queryOne(ctx, conn, "SELECT show_future_schedules FROM display_settings WHERE id = 1").Scan(&visible)
	if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
		return true, nil
	}
	if err != nil {
		return true, fmt.Errorf("read future schedules visibility: %w", err)
	}
	if visible != 0 && visible != 1 {
		return true, errors.New("invalid future schedules visibility")
	}
	return visible == 1, nil
}

// SetShowFutureSchedules updates only this preference; missing rows use the
// other preferences' defaults.
func (s *Store) SetShowFutureSchedules(ctx context.Context, visible bool) (err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish(&err)
	_, err = conn.ExecContext(ctx, `INSERT INTO display_settings(id, registration_form_visible, show_future_schedules)
 VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET show_future_schedules = excluded.show_future_schedules`,
		settings.DefaultDisplay().RegistrationFormVisible, visible)
	if err != nil {
		return fmt.Errorf("save future schedules visibility: %w", err)
	}
	return nil
}
