package application

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

type restoreIntegrityTestRows struct {
	driver.Rows
	values []string
	closes *int
}

func (r *restoreIntegrityTestRows) Next(row []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	row[0] = r.values[0]
	r.values = r.values[1:]
	return nil
}

func (r *restoreIntegrityTestRows) Close() error {
	*r.closes++
	return nil
}

func TestRestoreIntegrityRequiresExactlyOneOK(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
	}{
		{"no rows", nil},
		{"diagnostic", []string{"synthetic integrity diagnostic"}},
		{"extra row", []string{"ok", "ok"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := restoreSQLTestOwner(strings.NewReader("fixture"), 7)
			registration := &restoreSQLTestCloser{}
			queries, closes := 0, 0
			result, err := runRestoreSQL(context.Background(), p, restoreSQLOps{
				register: func(*restoreSQLFS) (string, io.Closer, error) { return "fixtureVFS", registration, nil },
				connect: func(string) (driver.Conn, error) {
					return restoreSQLTestConn{
						query: func() (driver.Rows, error) {
							queries++
							return &restoreIntegrityTestRows{values: tc.values, closes: &closes}, nil
						},
						close: func() error { return nil },
					}, nil
				},
				check: func(ctx context.Context, s *restoreSQLSession, r *restoreSQLResult) {
					s.checkIntegrity(ctx, r)
				},
			})
			if !errors.Is(err, errRestoreIntegrity) || result.ReleaseErr != nil ||
				queries != 1 || closes != 1 || registration.calls != 1 {
				t.Fatal(result, err, queries, closes, registration.calls)
			}
		})
	}
}
