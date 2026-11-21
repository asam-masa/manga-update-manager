package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func preparationGroup(t *testing.T, count int) *RestorePreparation {
	t.Helper()
	p := &RestorePreparation{work: outputWork(t)}
	for range count {
		f, err := p.CreateFile(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		if _, err = f.Write([]byte("synthetic")); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestRestorePreparationGroupNative(t *testing.T) {
	workDirectoryOps(t) // Capability gate for this synthetic NTFS fixture.
	root := workDirectoryRoot(t)
	p, err := CreateRestorePreparation(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Cleanup(nil) })
	for range 3 {
		if _, err = p.CreateFile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	r, err := p.Cleanup(context.Canceled)
	if err != nil || r.OriginalErr != context.Canceled || len(r.Children) != 3 || !r.Parent.Absent || !r.ParentRelease.Confirmed() {
		t.Fatal(r, err)
	}
	for _, c := range r.Children {
		if !c.Discard.Absent || !c.Release.Confirmed() || c.Err != nil {
			t.Fatal(c)
		}
	}
	if f, err := p.CreateFile(context.Background()); f != nil || !errors.Is(err, os.ErrClosed) {
		t.Fatal(f, err)
	}
	// Repetition must not act on later objects, or expose mutable cached slices.
	if err = os.Mkdir(r.Parent.Path, 0700); err != nil {
		t.Fatal(err)
	}
	later := r.Children[0].Path
	if err = os.WriteFile(later, []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Children[0].Path = "modified result"
	again, err := p.Cleanup(errors.New("later cause"))
	if !errors.Is(err, ErrRestorePreparationCleanupAttempted) || again.OriginalErr != context.Canceled || again.Children[0].Path != later {
		t.Fatal(again, err)
	}
	if got, err := os.ReadFile(later); err != nil || string(got) != "later" {
		t.Fatal("later object changed", err)
	}
}

func TestRestorePreparationGroupChildFailures(t *testing.T) {
	for _, mode := range []string{"identity", "mark", "close", "absence"} {
		for failedAt := range 3 {
			t.Run(mode+string(rune('0'+failedAt)), func(t *testing.T) {
				p := preparationGroup(t, 3)
				fault := errors.New("synthetic child fault")
				order := []string{}
				for _, child := range p.children {
					f := child.file
					closeFile := f.closeFile
					f.closeFile = func() error { order = append(order, f.Path()); return closeFile() }
				}
				closeParent := p.work.closeFile
				p.work.closeFile = func() error { order = append(order, "parent"); return closeParent() }
				i := 0
				ops := restorePreparationCleanupOperations()
				ops.discardChild = func(f *RestorePreparationFile) (RestorePreparationDiscardResult, error) {
					current := i
					i++
					childOps := restoreDiscardOps()
					if current == failedAt {
						switch mode {
						case "identity":
							childOps.identity = func(*os.File) (BackupTempIdentity, error) { return BackupTempIdentity{}, fault }
						case "mark":
							childOps.mark = func(*os.File) error { return fault }
						case "absence":
							childOps.absent = func(string) error { return fault }
						case "close":
							closeFile := f.closeFile
							f.closeFile = func() error { return errors.Join(closeFile(), fault) }
						}
					}
					return f.discard(childOps)
				}
				r, err := p.cleanup(context.Canceled, ops)
				if !errors.Is(err, fault) || errors.Is(err, context.Canceled) || r.OriginalErr != context.Canceled || r.DeletionStopped || r.Parent.MarkedForDeletion || !r.ParentRelease.Confirmed() || i != 3 {
					t.Fatal(r, err, i)
				}
				if len(order) != 4 || order[3] != "parent" {
					t.Fatal("release order", order)
				}
				for j, c := range r.Children {
					if j == failedAt {
						if !errors.Is(c.Err, fault) || c.Discard.Absent || c.Release.Confirmed() != (mode != "close") {
							t.Fatal(c)
						}
					} else if c.Err != nil || !c.Discard.Absent || !c.Release.Confirmed() {
						t.Fatal("remaining child not cleaned", c)
					}
				}
				if _, err := os.Stat(p.work.Path()); err != nil {
					t.Fatal("parent not preserved", err)
				}
				if _, err := p.cleanup(nil, ops); !errors.Is(err, ErrRestorePreparationCleanupAttempted) || !errors.Is(err, fault) || i != 3 || len(order) != 4 {
					t.Fatal("cleanup retried or lost failure", err, i, order)
				}
			})
		}
	}
}

func TestRestorePreparationGroupParentUncertainty(t *testing.T) {
	for failAt := range 4 { // Before each child, or immediately before the parent.
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			p := preparationGroup(t, 3)
			fault := errors.New("transient parent uncertainty")
			checks := 0
			ops := restorePreparationCleanupOperations()
			ops.verifyParent = func(work *BackupWorkDirectory) error {
				current := checks
				checks++
				if current == failAt {
					return fault
				}
				return work.VerifyIdentity() // Would succeed again: must not be called.
			}
			r, err := p.cleanup(nil, ops)
			if !errors.Is(err, fault) || !r.DeletionStopped || checks != failAt+1 || r.Parent.MarkedForDeletion || !r.ParentRelease.Confirmed() {
				t.Fatal(r, err, checks)
			}
			for i, c := range r.Children {
				if c.Discard.Absent != (i < failAt) || !c.Release.Confirmed() {
					t.Fatal(i, c)
				}
			}
		})
	}
	// Parent can also become unconfirmed inside the child's second check.
	p := preparationGroup(t, 3)
	ops := restorePreparationCleanupOperations()
	calls := 0
	ops.discardChild = func(f *RestorePreparationFile) (RestorePreparationDiscardResult, error) {
		calls++
		p.work.identity.FileID[0] ^= 1
		r, err := f.Discard()
		p.work.identity.FileID[0] ^= 1
		return r, err
	}
	r, err := p.cleanup(nil, ops)
	if !errors.Is(err, ErrRestorePreparationParentUnconfirmed) || !r.DeletionStopped || calls != 1 || !r.ParentRelease.Confirmed() {
		t.Fatal(r, err, calls)
	}
	for _, c := range r.Children {
		if c.Discard.MarkedForDeletion || !c.Release.Confirmed() {
			t.Fatal(c)
		}
	}
}

func TestRestorePreparationGroupFailedCreationAndProtectedFiles(t *testing.T) {
	p := preparationGroup(t, 1)
	fault := errors.New("created child could not be identified")
	_, err := p.createFile(context.Background(), func(ctx context.Context, work *BackupWorkDirectory) (*RestorePreparationFile, error) {
		return createRestorePreparationFile(ctx, work, [16]byte{}, restorePreparationFileOps{
			create:  createRestorePreparationFileHandle,
			observe: func(*os.File) (backupDBObservation, error) { return backupDBObservation{}, fault },
			close:   (*os.File).Close,
		})
	})
	if !errors.Is(err, fault) {
		t.Fatal(err)
	}
	var detail *RestorePreparationFileError
	if !errors.As(err, &detail) || !detail.Created {
		t.Fatal(err)
	}
	// These arbitrary files are never registered or removed, even if in work.
	protected := []string{detail.Path, filepath.Join(p.work.Path(), "unexpected"), filepath.Join(filepath.Dir(p.work.Path()), "source.zip"), filepath.Join(filepath.Dir(p.work.Path()), "current.db"), filepath.Join(filepath.Dir(p.work.Path()), "retired.db")}
	for _, path := range protected {
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := p.Cleanup(fault)
	if !errors.Is(err, fault) || len(r.Children) != 2 || !r.Children[0].Discard.Absent || r.Children[1].Discard.MarkedForDeletion || r.Parent.MarkedForDeletion || !r.ParentRelease.Confirmed() {
		t.Fatal(r, err)
	}
	for _, path := range protected {
		if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
			t.Fatal(path, err)
		}
	}
}

func TestRestorePreparationGroupParentFailures(t *testing.T) {
	for _, mode := range []string{"nonempty", "mark", "close", "absence", "ancestors", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			p := preparationGroup(t, 1)
			fault := errors.New("synthetic parent fault")
			if mode == "nonempty" {
				if err := os.WriteFile(filepath.Join(p.work.Path(), "unknown"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "close" || mode == "multiple" {
				closeFile := p.work.closeFile
				p.work.closeFile = func() error { return errors.Join(closeFile(), fault) }
			}
			if mode == "ancestors" || mode == "multiple" {
				parent := p.work.parent
				p.work.parent = backupTestCloser(func() error { return errors.Join(parent.Close(), fault) })
			}
			if mode == "multiple" {
				f := p.children[0].file
				closeFile := f.closeFile
				f.closeFile = func() error { return errors.Join(closeFile(), fault) }
			}
			ops := restorePreparationCleanupOperations()
			ops.discardParent = func(work *BackupWorkDirectory) (BackupWorkDirectoryDiscardResult, error) {
				childOps := backupWorkDiscardOps{identifyBackupWorkDirectory, backupHandleName, markBackupTempDeleted, confirmBackupWorkDirectoryAbsent}
				if mode == "mark" {
					childOps.mark = func(*os.File) error { return fault }
				}
				if mode == "absence" {
					childOps.absent = func(string) error { return fault }
				}
				return work.discardEmpty(childOps)
			}
			r, err := p.cleanup(nil, ops)
			if err == nil || r.ParentRelease.Confirmed() != (mode == "nonempty" || mode == "mark" || mode == "absence") {
				t.Fatal(r, err)
			}
			if mode != "nonempty" && !errors.Is(err, fault) {
				t.Fatal("lost cause", err)
			}
			if mode == "multiple" && (r.Children[0].Release.Confirmed() || r.Parent.MarkedForDeletion) {
				t.Fatal("partial failures lost", r)
			}
		})
	}
}

func TestRestorePreparationGroupAdmissionAndIsolation(t *testing.T) {
	for _, p := range []*RestorePreparation{nil, {}} {
		if _, err := p.CreateFile(context.Background()); !errors.Is(err, os.ErrClosed) {
			t.Fatal(err)
		}
		if _, err := p.Cleanup(nil); !errors.Is(err, os.ErrClosed) {
			t.Fatal(err)
		}
	}
	p := preparationGroup(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.CreateFile(ctx); !errors.Is(err, context.Canceled) || len(p.children) != 0 {
		t.Fatal(err)
	}
	other := preparationGroup(t, 1)
	r, err := p.Cleanup(context.Canceled)
	if err != nil || !r.Parent.Absent {
		t.Fatal(r, err)
	}
	if size, err := other.children[0].file.Size(); err != nil || size != 9 {
		t.Fatal("foreign owner changed", size, err)
	}
	if _, err := other.Cleanup(nil); err != nil {
		t.Fatal(err)
	}
	// Creates racing with cleanup either join this run or are rejected.
	p = preparationGroup(t, 0)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := p.CreateFile(context.Background()); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
		})
	}
	r, err = p.Cleanup(nil)
	wg.Wait()
	if err != nil || !r.Parent.Absent || len(r.Children) != len(p.children) {
		t.Fatal(r, err)
	}
}
