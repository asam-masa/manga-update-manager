package application

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"

	"modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

type restoreSQLResource uint8

const (
	restoreSQLNotAcquired restoreSQLResource = iota
	restoreSQLAcquired
	restoreSQLReleased
	restoreSQLUnknown
)

type restoreSQLSession struct {
	fs          *restoreSQLFS
	vfs         io.Closer
	db          driver.Conn
	rows        driver.Rows
	observation restoreSQLResult
}

type restoreSQLResult struct {
	Rows, DB, VFS                                 restoreSQLResource
	OpenViews                                     int
	OperationErr, ReadErr, ContextErr, ReleaseErr error
}

func (r restoreSQLResult) err() error {
	return errors.Join(r.OperationErr, r.ReadErr, r.ContextErr, r.ReleaseErr)
}

type restoreSQLOps struct {
	register func(*restoreSQLFS) (string, io.Closer, error)
	connect  func(string) (driver.Conn, error)
}

// probePreparedRestoreSQL exercises the connection with a fixed internal query.
// It makes no decision about whether the database is compatible for restore.
func probePreparedRestoreSQL(ctx context.Context, p *PreparedRestore) (restoreSQLResult, error) {
	return runRestoreSQL(ctx, p, restoreSQLOps{
		register: func(f *restoreSQLFS) (string, io.Closer, error) {
			name, registration, err := vfs.New(f)
			if registration == nil {
				return name, nil, err
			}
			return name, registration, err
		},
		connect: (&sqlite.Driver{}).Open,
	})
}

func runRestoreSQL(ctx context.Context, p *PreparedRestore, ops restoreSQLOps) (result restoreSQLResult, err error) {
	if err := ctx.Err(); err != nil {
		result.ContextErr = err
		return result, err
	}
	use, err := p.BeginSQLUse()
	if err != nil {
		result.OperationErr = err
		return result, err
	}
	session := &restoreSQLSession{}
	defer func() {
		session.release(&result)
		result.ContextErr = ctx.Err()
		confirmed := result.ReleaseErr == nil
		if !confirmed {
			session.observation = result
			p.mu.Lock()
			p.retainedSQL = session
			p.mu.Unlock()
		}
		endErr := use.End(confirmed, result.ReleaseErr)
		err = errors.Join(result.err(), endErr)
	}()
	for _, file := range p.files {
		if file.ImageID == "" && file.Metadata == p.manifest.Database && file.Reader != nil && file.Metadata.Size > 0 {
			session.fs = &restoreSQLFS{ctx: ctx, reader: file.Reader, size: file.Metadata.Size}
			break
		}
	}
	if session.fs == nil {
		result.OperationErr = errors.New("prepared database is missing")
		return
	}
	session.run(ctx, ops, &result)
	return
}

func (s *restoreSQLSession) run(ctx context.Context, ops restoreSQLOps, result *restoreSQLResult) {
	if ctx.Err() != nil {
		return
	}
	name, registration, err := ops.register(s.fs)
	s.vfs = registration
	if err != nil || registration == nil || name == "" {
		result.VFS = restoreSQLUnknown
		result.OperationErr = errors.Join(errors.New("restore VFS registration failed"), err)
		return
	}
	result.VFS = restoreSQLAcquired
	if ctx.Err() != nil {
		return
	}
	dsn := "file:" + restoreSQLName + "?" + url.Values{"mode": {"ro"}, "vfs": {name}}.Encode()
	s.db, err = ops.connect(dsn)
	if err != nil || s.db == nil {
		result.DB = restoreSQLUnknown
		result.OperationErr = errors.Join(errors.New("restore SQL connection failed"), err)
		return
	}
	result.DB = restoreSQLAcquired
	if s.failure() != nil || ctx.Err() != nil {
		return
	}
	query, ok := s.db.(driver.QueryerContext)
	if !ok {
		result.OperationErr = errors.New("restore driver lacks contextual query support")
		return
	}
	s.rows, err = query.QueryContext(ctx, "SELECT count(*) FROM sqlite_schema", nil)
	if err != nil || s.rows == nil {
		result.Rows = restoreSQLUnknown
		result.OperationErr = errors.Join(errors.New("restore SQL query failed"), err)
		return
	}
	result.Rows = restoreSQLAcquired
	values := make([]driver.Value, 1)
	if s.failure() == nil && ctx.Err() == nil {
		if err := s.rows.Next(values); err != nil {
			result.OperationErr = err
		} else if s.failure() == nil && ctx.Err() == nil {
			if err := s.rows.Next(values); err != io.EOF {
				result.OperationErr = errors.Join(errors.New("expected one restore SQL result row"), err)
			}
		}
	}
}

func (s *restoreSQLSession) failure() error {
	s.fs.mu.Lock()
	defer s.fs.mu.Unlock()
	return s.fs.fault
}

func (s *restoreSQLSession) release(r *restoreSQLResult) {
	closeResource := func(name string, resource io.Closer, state *restoreSQLResource) {
		if resource != nil {
			if err := resource.Close(); err != nil {
				*state = restoreSQLUnknown
				r.ReleaseErr = errors.Join(r.ReleaseErr, fmt.Errorf("close restore %s: %w", name, err))
			} else if *state == restoreSQLAcquired {
				*state = restoreSQLReleased
			}
		}
		if *state == restoreSQLUnknown {
			r.ReleaseErr = errors.Join(r.ReleaseErr, fmt.Errorf("restore %s release unconfirmed", name))
		}
	}
	closeResource("rows", s.rows, &r.Rows)
	closeResource("DB", s.db, &r.DB)
	if r.Rows == restoreSQLUnknown && r.DB == restoreSQLReleased {
		// sqlite3_close_v2 can defer destruction while a statement is live.
		r.DB = restoreSQLUnknown
		r.ReleaseErr = errors.Join(r.ReleaseErr, errors.New("restore DB release unconfirmed after rows failure"))
	}
	if s.fs != nil {
		s.fs.mu.Lock()
		r.OpenViews = s.fs.openViews
		r.ReadErr = s.fs.fault
		s.fs.mu.Unlock()
	}
	if r.OpenViews != 0 {
		r.ReleaseErr = errors.Join(r.ReleaseErr, fmt.Errorf("%d restore VFS views still open", r.OpenViews))
	}
	if r.Rows != restoreSQLUnknown && (r.DB == restoreSQLNotAcquired || r.DB == restoreSQLReleased) && r.OpenViews == 0 {
		closeResource("VFS", s.vfs, &r.VFS)
	}
}
