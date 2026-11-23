package application

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func preparedRestoreStateFixture(cleanup func(error) (platform.RestorePreparationCleanupResult, error)) *PreparedRestore {
	return &PreparedRestore{cleanup: cleanup}
}

func TestPreparedRestoreSQLUseAndCleanup(t *testing.T) {
	var cleanupCalls atomic.Int32
	p := preparedRestoreStateFixture(func(error) (platform.RestorePreparationCleanupResult, error) {
		cleanupCalls.Add(1)
		return platform.RestorePreparationCleanupResult{}, nil
	})
	use, err := p.BeginSQLUse()
	if err != nil || use == nil {
		t.Fatal(use, err)
	}
	if other, err := p.BeginSQLUse(); other != nil || !errors.Is(err, ErrPreparedRestoreSQLInUse) {
		t.Fatal(other, err)
	}
	if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLInUse) || cleanupCalls.Load() != 0 {
		t.Fatal(err, cleanupCalls.Load())
	}
	if err := use.End(true, errors.New("ignored when confirmed")); err != nil {
		t.Fatal(err)
	}
	if err := use.End(true, nil); !errors.Is(err, ErrPreparedRestoreSQLUseEnded) {
		t.Fatal(err)
	}
	if _, err := p.Cleanup(nil); err != nil || cleanupCalls.Load() != 1 {
		t.Fatal(err, cleanupCalls.Load())
	}
	if use, err := p.BeginSQLUse(); use != nil || !errors.Is(err, ErrPreparedRestoreCleanupStarted) {
		t.Fatal(use, err)
	}
	if _, err := p.Cleanup(nil); err != nil || cleanupCalls.Load() != 2 {
		t.Fatal(err, cleanupCalls.Load())
	}
}

func TestPreparedRestoreSQLReleaseUnconfirmedIsSticky(t *testing.T) {
	fault := errors.New("synthetic VFS release fault")
	var cleanupCalls atomic.Int32
	p := preparedRestoreStateFixture(func(error) (platform.RestorePreparationCleanupResult, error) {
		cleanupCalls.Add(1)
		return platform.RestorePreparationCleanupResult{}, nil
	})
	use, err := p.BeginSQLUse()
	if err != nil {
		t.Fatal(err)
	}
	if err := use.End(false, fault); !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) || !errors.Is(err, fault) {
		t.Fatal(err)
	}
	if next, err := p.BeginSQLUse(); next != nil || !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) || !errors.Is(err, fault) {
		t.Fatal(next, err)
	}
	if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) || !errors.Is(err, fault) || cleanupCalls.Load() != 0 {
		t.Fatal(err, cleanupCalls.Load())
	}
	if err := use.End(true, nil); !errors.Is(err, ErrPreparedRestoreSQLUseEnded) {
		t.Fatal(err)
	}
}

func TestPreparedRestoreSQLUseCleanupRace(t *testing.T) {
	for i := 0; i < 100; i++ {
		var cleanupCalls atomic.Int32
		p := preparedRestoreStateFixture(func(error) (platform.RestorePreparationCleanupResult, error) {
			cleanupCalls.Add(1)
			return platform.RestorePreparationCleanupResult{}, nil
		})
		start := make(chan struct{})
		var use *PreparedRestoreSQLUse
		var beginErr, cleanupErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			use, beginErr = p.BeginSQLUse()
		}()
		go func() {
			defer wg.Done()
			<-start
			_, cleanupErr = p.Cleanup(nil)
		}()
		close(start)
		wg.Wait()

		switch {
		case beginErr == nil:
			if use == nil || !errors.Is(cleanupErr, ErrPreparedRestoreSQLInUse) || cleanupCalls.Load() != 0 {
				t.Fatal(i, use, beginErr, cleanupErr, cleanupCalls.Load())
			}
			if err := use.End(true, nil); err != nil {
				t.Fatal(i, err)
			}
		case cleanupErr == nil:
			if !errors.Is(beginErr, ErrPreparedRestoreCleanupStarted) || cleanupCalls.Load() != 1 {
				t.Fatal(i, use, beginErr, cleanupErr, cleanupCalls.Load())
			}
		default:
			t.Fatal(i, use, beginErr, cleanupErr, cleanupCalls.Load())
		}
	}
}

func TestPreparedRestoreSQLUseEndCleanupRace(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		t.Run(map[bool]string{true: "confirmed", false: "unconfirmed"}[confirmed], func(t *testing.T) {
			for i := 0; i < 100; i++ {
				fault := errors.New("synthetic release observation")
				var cleanupCalls atomic.Int32
				p := preparedRestoreStateFixture(func(error) (platform.RestorePreparationCleanupResult, error) {
					cleanupCalls.Add(1)
					return platform.RestorePreparationCleanupResult{}, nil
				})
				use, err := p.BeginSQLUse()
				if err != nil {
					t.Fatal(err)
				}
				start := make(chan struct{})
				var endErr, cleanupErr error
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					endErr = use.End(confirmed, fault)
				}()
				go func() {
					defer wg.Done()
					<-start
					_, cleanupErr = p.Cleanup(nil)
				}()
				close(start)
				wg.Wait()

				if confirmed {
					if endErr != nil {
						t.Fatal(i, endErr)
					}
					if cleanupErr == nil && cleanupCalls.Load() != 1 || errors.Is(cleanupErr, ErrPreparedRestoreSQLInUse) && cleanupCalls.Load() != 0 {
						t.Fatal(i, cleanupErr, cleanupCalls.Load())
					}
					if cleanupErr != nil && !errors.Is(cleanupErr, ErrPreparedRestoreSQLInUse) {
						t.Fatal(i, cleanupErr)
					}
				} else {
					if !errors.Is(endErr, ErrPreparedRestoreSQLReleaseUnconfirmed) || !errors.Is(endErr, fault) {
						t.Fatal(i, endErr)
					}
					if cleanupCalls.Load() != 0 || !errors.Is(cleanupErr, ErrPreparedRestoreSQLInUse) && !errors.Is(cleanupErr, ErrPreparedRestoreSQLReleaseUnconfirmed) {
						t.Fatal(i, cleanupErr, cleanupCalls.Load())
					}
				}
			}
		})
	}
}

func TestPreparedRestoreStaleSQLUseCannotEndNewUse(t *testing.T) {
	p := preparedRestoreStateFixture(func(error) (platform.RestorePreparationCleanupResult, error) {
		return platform.RestorePreparationCleanupResult{}, nil
	})
	first, err := p.BeginSQLUse()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.End(true, nil); err != nil {
		t.Fatal(err)
	}
	second, err := p.BeginSQLUse()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.End(true, nil); !errors.Is(err, ErrPreparedRestoreSQLUseEnded) {
		t.Fatal(err)
	}
	if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLInUse) {
		t.Fatal(err)
	}
	if err := second.End(true, nil); err != nil {
		t.Fatal(err)
	}
}
