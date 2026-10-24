package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

// WorkListMode reads this preference independently of form visibility.
// Neither missing nor unknown data authorizes a repair write.
func (s *Store) WorkListMode(ctx context.Context) (_ settings.WorkListMode, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return settings.Cards, err
	}
	defer finish(&err)
	var mode settings.WorkListMode
	err = queryOne(ctx, conn, "SELECT work_list_mode FROM display_settings WHERE id = 1").Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
		return settings.Cards, nil
	}
	if err != nil {
		return settings.Cards, fmt.Errorf("read work list mode: %w", err)
	}
	if err := mode.Validate(); err != nil {
		return settings.Cards, err
	}
	return mode, nil
}

func (s *Store) SetWorkListMode(ctx context.Context, mode settings.WorkListMode) (err error) {
	if err := mode.Validate(); err != nil {
		return err
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish(&err)
	_, err = conn.ExecContext(ctx, `INSERT INTO display_settings(id, registration_form_visible, work_list_mode)
 VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET work_list_mode = excluded.work_list_mode`,
		settings.DefaultDisplay().RegistrationFormVisible, string(mode))
	if err != nil {
		return fmt.Errorf("save work list mode: %w", err)
	}
	return nil
}
