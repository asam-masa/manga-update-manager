package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackupDBTransitionNative(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id"} {
		t.Run(mode, func(t *testing.T) {
			name := filepath.Join(workDirectoryRoot(t), "copy.sqlite")
			parent, err := HoldBackupDirectory(context.Background(), name)
			if err != nil {
				t.Fatal(err)
			}
			defer parent.Close()
			data, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			if _, err := data.WriteString("incomplete SQLite output"); err != nil {
				t.Fatal(err)
			}
			id, err := identifyBackupTemp(data)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "wrong-id" {
				id.FileID[0] ^= 1
			}
			owner, result, err := transitionBackupDB(data, id, name)
			if owner != nil {
				defer owner.Close()
			}
			if !result.dataClosed || result.path != name {
				t.Fatal(result, err)
			}
			if _, err := data.Stat(); !errors.Is(err, os.ErrClosed) && !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				t.Fatal("data not released", err)
			}
			if mode == "wrong-id" {
				if owner != nil || !errors.Is(err, errBackupDBTransition) || result.baselineConfirmed {
					t.Fatal(result, err)
				}
				if _, err := os.Stat(name); err != nil {
					t.Fatal("artifact lost", err)
				}
				return
			}
			if err != nil || owner == nil || !result.baselineConfirmed || !result.referenceVerified || result.referenceClosed {
				t.Fatal(result, err)
			}
			if result, err := owner.Discard(); err != nil || !result.absent {
				t.Fatal(result, err)
			}
		})
	}
}

func TestBackupDBTransitionFaults(t *testing.T) {
	fault, dataCloseFault, refCloseFault := errors.New("stage"), errors.New("data close"), errors.New("ref close")
	for _, mode := range []string{"success", "baseline", "reference", "identity", "wrong-id", "data-close", "acquire", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			data, reference := new(os.File), new(os.File)
			id := BackupTempIdentity{VolumeSerial: 1}
			wantOwner := new(backupDBCleanup)
			baseline := backupDBBaseline{size: 3}
			var calls []string
			ops := backupDBTransitionOps{
				baseline: func(f *os.File, gotID BackupTempIdentity, name string) (backupDBBaseline, error) {
					calls = append(calls, "baseline")
					if f != data || gotID != id || name != "candidate" {
						t.Fatal("wrong baseline inputs")
					}
					if mode == "baseline" {
						return backupDBBaseline{}, fault
					}
					return baseline, nil
				},
				reference: func(string) (*os.File, error) {
					calls = append(calls, "reference")
					if mode == "reference" {
						return nil, fault
					}
					return reference, nil
				},
				identity: func(f *os.File) (BackupTempIdentity, error) {
					calls = append(calls, "identity")
					if f != reference {
						t.Fatal("wrong identity input")
					}
					if mode == "identity" || mode == "multiple" {
						return BackupTempIdentity{}, fault
					}
					if mode == "wrong-id" {
						return BackupTempIdentity{}, nil
					}
					return id, nil
				},
				close: func(f *os.File) error {
					if f == data {
						calls = append(calls, "data-close")
						if mode == "data-close" || mode == "multiple" {
							return dataCloseFault
						}
					} else if f == reference {
						calls = append(calls, "ref-close")
						if mode == "multiple" {
							return refCloseFault
						}
					} else {
						t.Fatal("wrong close")
					}
					return nil
				},
				acquire: func(name string, ref *os.File, gotID BackupTempIdentity, gotBaseline backupDBBaseline) (*backupDBCleanup, error) {
					calls = append(calls, "acquire")
					if name != "candidate" || ref != reference || gotID != id || gotBaseline != baseline {
						t.Fatal("wrong acquire inputs")
					}
					if mode == "acquire" {
						return nil, fault
					}
					return wantOwner, nil
				},
			}
			owner, result, err := moveBackupDBToCleanup(data, id, "candidate", ops)
			if result.referenceAcquired != (mode != "baseline" && mode != "reference") || result.cleanupAcquisitionKnown != (mode != "acquire") || result.cleanupAcquired != (mode == "success") || result.cleanupClosed {
				t.Fatalf("incorrect handle acquisition observation: %+v", result)
			}
			want := []string{"baseline"}
			if mode != "baseline" {
				want = append(want, "reference")
			}
			if mode != "baseline" && mode != "reference" {
				want = append(want, "identity")
			}
			want = append(want, "data-close")
			if mode == "success" || mode == "acquire" {
				want = append(want, "acquire")
			}
			if mode != "success" && mode != "baseline" && mode != "reference" {
				want = append(want, "ref-close")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal(calls, want)
			}
			if mode == "success" {
				if err != nil || owner != wantOwner || result.referenceClosed {
					t.Fatal(owner, result, err)
				}
			} else {
				if owner != nil || !errors.Is(err, errBackupDBTransition) {
					t.Fatal(owner, result, err)
				}
				if mode != "wrong-id" && mode != "data-close" && !errors.Is(err, fault) {
					t.Fatal("lost stage cause", err)
				}
			}
			if result.dataClosed != (mode != "data-close" && mode != "multiple") {
				t.Fatal(result)
			}
			if (mode == "data-close" || mode == "multiple") && !errors.Is(err, dataCloseFault) {
				t.Fatal("lost close cause", err)
			}
			if mode == "multiple" && !errors.Is(err, refCloseFault) {
				t.Fatal("lost reference close cause", err)
			}
		})
	}
}

func TestBackupDBTransitionReacquisitionDetails(t *testing.T) {
	for _, mode := range []string{"unacquired", "released", "unreleased", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			data, reference := new(os.File), new(os.File)
			cause, closeFault := errors.New("reacquisition cause"), errors.New("candidate close failed")
			dataCloses, refCloses := 0, 0
			id := BackupTempIdentity{VolumeSerial: 1}
			ops := backupDBTransitionOps{
				baseline:  func(*os.File, BackupTempIdentity, string) (backupDBBaseline, error) { return backupDBBaseline{}, nil },
				reference: func(string) (*os.File, error) { return reference, nil },
				identity:  func(*os.File) (BackupTempIdentity, error) { return id, nil },
				close: func(f *os.File) error {
					if f == data {
						dataCloses++
					} else if f == reference {
						refCloses++
					} else {
						t.Fatal("unexpected close")
					}
					return nil
				},
				acquire: func(string, *os.File, BackupTempIdentity, backupDBBaseline) (*backupDBCleanup, error) {
					if mode == "unknown" {
						return nil, cause
					}
					err := error(cause)
					if mode == "unreleased" {
						err = errors.Join(err, closeFault)
					}
					return nil, errors.Join(errBackupDBReacquire, &backupDBReacquireError{acquired: mode != "unacquired", closed: mode == "released", err: err})
				},
			}
			owner, tr, err := moveBackupDBToCleanup(data, id, "candidate", ops)
			if owner != nil || !errors.Is(err, cause) || !errors.Is(err, errBackupDBTransition) || dataCloses != 1 || refCloses != 1 {
				t.Fatal(owner, tr, err, dataCloses, refCloses)
			}
			if mode == "unreleased" && !errors.Is(err, closeFault) {
				t.Fatal("lost candidate release cause", err)
			}
			r := publicBackupDatabaseTransition(tr)
			if !r.DataClosed || !r.ReferenceAcquired || !r.ReferenceClosed || r.CleanupHandleAcquisitionKnown != (mode != "unknown") || r.CleanupHandleAcquired != (mode == "released" || mode == "unreleased") || r.CleanupHandleClosed != (mode == "released") {
				t.Fatalf("incorrect public observations: %+v", r)
			}
		})
	}
}
