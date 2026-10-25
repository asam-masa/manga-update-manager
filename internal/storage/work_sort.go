package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

// WorkSort reads only this preference and never inserts or repairs a row.
func (s *Store) WorkSort(ctx context.Context) (_ settings.WorkSort, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return settings.RegistrationAsc, err
	}
	defer finish(&err)
	var order settings.WorkSort
	err = queryOne(ctx, conn, "SELECT work_sort FROM display_settings WHERE id = 1").Scan(&order)
	if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
		return settings.RegistrationAsc, nil
	}
	if err != nil {
		return settings.RegistrationAsc, fmt.Errorf("read work sort: %w", err)
	}
	if err := order.Validate(); err != nil {
		return settings.RegistrationAsc, err
	}
	return order, nil
}

func (s *Store) SetWorkSort(ctx context.Context, order settings.WorkSort) (err error) {
	if err := order.Validate(); err != nil {
		return err
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish(&err)
	_, err = conn.ExecContext(ctx, `INSERT INTO display_settings(id, registration_form_visible, work_sort)
 VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET work_sort = excluded.work_sort`,
		settings.DefaultDisplay().RegistrationFormVisible, string(order))
	if err != nil {
		return fmt.Errorf("save work sort: %w", err)
	}
	return nil
}
