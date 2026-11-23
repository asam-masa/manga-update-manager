package application

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"modernc.org/sqlite/vfs"
)

func restoreSQLTestOwner(reader io.ReaderAt, size int64) *PreparedRestore {
	meta := backup.File{Path: "database.sqlite", Size: size}
	return &PreparedRestore{
		manifest: backup.Manifest{Database: meta},
		files:    []PreparedRestoreFile{{Metadata: meta, Reader: reader}},
	}
}

func TestRestoreSQLSessionReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE example (id INTEGER PRIMARY KEY)")
	if err := errors.Join(err, db.Close()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newRestoreFixtureWithDB(t, data)
	p, err := PrepareRestore(context.Background(), fixture.source, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Cleanup(nil); err != nil {
			t.Error(err)
		}
		fixture.assertProtected(t)
	})
	result, err := probePreparedRestoreSQL(context.Background(), p)
	if err != nil || result.Rows != restoreSQLReleased || result.DB != restoreSQLReleased || result.VFS != restoreSQLReleased || result.OpenViews != 0 {
		t.Fatal(result, err)
	}
	use, err := p.BeginSQLUse()
	if err != nil {
		t.Fatal(err)
	}
	if err := use.End(true, nil); err != nil {
		t.Fatal(err)
	}
	borrowedDB := p.Files()[0]
	got, err := io.ReadAll(io.NewSectionReader(borrowedDB.Reader, 0, borrowedDB.Metadata.Size))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("prepared database changed or closed", err)
	}

	borrowed := &restoreSQLFS{ctx: context.Background(), reader: borrowedDB.Reader, size: borrowedDB.Metadata.Size}
	use, err = p.BeginSQLUse()
	if err != nil {
		t.Fatal(err)
	}
	name, registration, err := vfs.New(borrowed)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := registration.Close()
		if endErr := use.End(err == nil && borrowed.openViews == 0, err); endErr != nil {
			t.Error(endErr)
		}
	}()
	dsn := "file:" + restoreSQLName + "?" + url.Values{"mode": {"ro"}, "vfs": {name}}.Encode()
	readOnly, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := readOnly.Close(); err != nil {
			t.Error(err)
		}
	}()
	var count int
	if err := readOnly.QueryRow("SELECT count(*) FROM example").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	_, err = readOnly.Exec("INSERT INTO example VALUES (1)")
	var sqliteErr interface{ Code() int }
	if !errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != 8 {
		t.Fatal("expected SQLITE_READONLY", err)
	}
}

type restoreSQLTestCloser struct {
	calls int
	err   error
}

func (c *restoreSQLTestCloser) Close() error {
	c.calls++
	return c.err
}

func TestRestoreSQLSessionPreservesUnknownConnection(t *testing.T) {
	p := restoreSQLTestOwner(bytes.NewReader([]byte("fixture")), 7)
	registration := &restoreSQLTestCloser{}
	fault := errors.New("connection acquisition fault")
	result, err := runRestoreSQL(context.Background(), p, restoreSQLOps{
		register: func(*restoreSQLFS) (string, io.Closer, error) {
			return "fixtureVFS", registration, nil
		},
		connect: func(string) (driver.Conn, error) {
			if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLInUse) {
				t.Error(err)
			}
			return nil, fault
		},
	})
	if !errors.Is(err, fault) || !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) ||
		result.DB != restoreSQLUnknown || result.VFS != restoreSQLAcquired || registration.calls != 0 ||
		p.retainedSQL == nil || p.retainedSQL.vfs != registration {
		t.Fatal(result, err, registration.calls)
	}
	if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) {
		t.Fatal(err)
	}
}

type restoreSQLTestConn struct {
	driver.Conn
	query func() (driver.Rows, error)
	close func() error
}

func (c restoreSQLTestConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return c.query()
}
func (c restoreSQLTestConn) Close() error { return c.close() }

type restoreSQLTestRows struct {
	driver.Rows
	next  func() error
	close func() error
}

func (r restoreSQLTestRows) Next([]driver.Value) error { return r.next() }
func (r restoreSQLTestRows) Close() error              { return r.close() }

func TestRestoreSQLSessionCancellationAndRelease(t *testing.T) {
	for _, failClose := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		p := restoreSQLTestOwner(bytes.NewReader([]byte("fixture")), 7)
		registration := &restoreSQLTestCloser{}
		fault := errors.New("synthetic DB close failure")
		rowsClosed, calls := false, 0
		result, err := runRestoreSQL(ctx, p, restoreSQLOps{
			register: func(*restoreSQLFS) (string, io.Closer, error) { return "fixtureVFS", registration, nil },
			connect: func(string) (driver.Conn, error) {
				return restoreSQLTestConn{
					query: func() (driver.Rows, error) {
						return restoreSQLTestRows{
							next:  func() error { calls++; cancel(); return nil },
							close: func() error { rowsClosed = true; return nil },
						}, nil
					},
					close: func() error {
						if !rowsClosed {
							t.Error("DB closed before rows")
						}
						if failClose {
							return fault
						}
						return nil
					},
				}, nil
			},
		})
		cancel()
		if !errors.Is(err, context.Canceled) || calls != 1 || result.Rows != restoreSQLReleased {
			t.Fatal(result, err, calls)
		}
		if failClose {
			if !errors.Is(err, fault) || result.DB != restoreSQLUnknown || registration.calls != 0 {
				t.Fatal(result, err, registration.calls)
			}
		} else if result.DB != restoreSQLReleased || result.VFS != restoreSQLReleased || registration.calls != 1 {
			t.Fatal(result, err, registration.calls)
		}
	}
}

func TestRestoreSQLSessionRejectsSuccessWithReadFault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fault := errors.New("full read with fault")
	reader := &restoreSQLFaultReader{err: fault}
	p := restoreSQLTestOwner(reader, 3)
	registration := &restoreSQLTestCloser{}
	var borrowed *restoreSQLFS
	nextCalls := 0
	result, err := runRestoreSQL(ctx, p, restoreSQLOps{
		register: func(f *restoreSQLFS) (string, io.Closer, error) {
			borrowed = f
			return "fixtureVFS", registration, nil
		},
		connect: func(string) (driver.Conn, error) {
			return restoreSQLTestConn{
				query: func() (driver.Rows, error) {
					return restoreSQLTestRows{
						next: func() error {
							nextCalls++
							n, _ := borrowed.ReadAt(make([]byte, 3), 0)
							if n != 3 {
								t.Error(n)
							}
							cancel()
							return nil
						},
						close: func() error { return nil },
					}, nil
				},
				close: func() error { return nil },
			}, nil
		},
	})
	if !errors.Is(err, fault) || !errors.Is(result.ReadErr, fault) || result.OperationErr != nil ||
		result.ReleaseErr != nil || !errors.Is(result.ContextErr, context.Canceled) || nextCalls != 1 || registration.calls != 1 {
		t.Fatal(result, err, nextCalls, registration.calls)
	}
}

func TestRestoreSQLSessionUnconfirmedRelease(t *testing.T) {
	for _, mode := range []string{"rows", "vfs", "view"} {
		t.Run(mode, func(t *testing.T) {
			p := restoreSQLTestOwner(bytes.NewReader([]byte("fixture")), 7)
			registration := &restoreSQLTestCloser{}
			if mode == "vfs" {
				registration.err = vfs.ErrInUse
			}
			rowsFault := errors.New("rows close fault")
			var leaked fs.File
			result, err := runRestoreSQL(context.Background(), p, restoreSQLOps{
				register: func(f *restoreSQLFS) (string, io.Closer, error) {
					if mode == "view" {
						var err error
						leaked, err = f.Open(restoreSQLName)
						if err != nil {
							t.Error(err)
						}
					}
					return "fixtureVFS", registration, nil
				},
				connect: func(string) (driver.Conn, error) {
					return restoreSQLTestConn{
						query: func() (driver.Rows, error) {
							first := true
							return restoreSQLTestRows{
								next: func() error {
									if first {
										first = false
										return nil
									}
									return io.EOF
								},
								close: func() error {
									if mode == "rows" {
										return rowsFault
									}
									return nil
								},
							}, nil
						},
						close: func() error { return nil },
					}, nil
				},
			})
			if leaked != nil {
				defer leaked.Close()
			}
			if !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) || p.retainedSQL == nil {
				t.Fatal(result, err)
			}
			switch mode {
			case "rows":
				if !errors.Is(err, rowsFault) || result.Rows != restoreSQLUnknown || result.DB != restoreSQLUnknown || registration.calls != 0 {
					t.Fatal(result, err)
				}
			case "vfs":
				if !errors.Is(err, vfs.ErrInUse) || result.DB != restoreSQLReleased || result.VFS != restoreSQLUnknown || registration.calls != 1 {
					t.Fatal(result, err)
				}
			case "view":
				if result.DB != restoreSQLReleased || result.OpenViews != 1 || registration.calls != 0 {
					t.Fatal(result, err)
				}
			}
			if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) {
				t.Fatal(err)
			}
			if _, err := p.BeginSQLUse(); !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) {
				t.Fatal(err)
			}
		})
	}
}
