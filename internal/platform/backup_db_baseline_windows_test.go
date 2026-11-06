package platform

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupDBBaselineRetainedFile(t *testing.T) {
	for _, data := range []string{"", "incomplete SQLite output"} {
		t.Run(data, func(t *testing.T) {
			name := filepath.Join(workDirectoryRoot(t), "copy.sqlite")
			f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString(data); err != nil {
				t.Fatal(err)
			}
			id, err := identifyBackupTemp(f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := captureBackupDBBaseline(f, id, name)
			if err != nil || got.size != int64(len(data)) || got.hash != sha256.Sum256([]byte(data)) {
				t.Fatal(got, err)
			}
			pos, err := f.Seek(0, io.SeekCurrent)
			if err != nil || pos != int64(len(data)) {
				t.Fatal("position changed", pos, err)
			}
			if _, err := f.Stat(); err != nil {
				t.Fatal("handle closed", err)
			}
			if data, err := os.ReadFile(name); err != nil || int64(len(data)) != got.size {
				t.Fatal("file not preserved", err)
			}
			wrong := id
			wrong.FileID[0] ^= 1
			if got, err := captureBackupDBBaseline(f, wrong, name); !errors.Is(err, errBackupDBBaseline) || got != (backupDBBaseline{}) {
				t.Fatal(got, err)
			}
			if _, err := captureBackupDBBaseline(f, id, name+"-other"); err == nil {
				t.Fatal("wrong name accepted")
			}
		})
	}
}

func TestBackupDBBaselineRejectsHardlinkAndClosed(t *testing.T) {
	name := filepath.Join(workDirectoryRoot(t), "copy.sqlite")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id, err := identifyBackupTemp(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(name, name+"-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := captureBackupDBBaseline(f, id, name); err == nil {
		t.Fatal("hardlink accepted")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := captureBackupDBBaseline(f, id, name); err == nil {
		t.Fatal("closed handle accepted")
	}
}

type baselineErrorReader struct{ err error }

func (r baselineErrorReader) ReadAt([]byte, int64) (int, error) { return 0, r.err }

func TestBackupDBBaselineFaults(t *testing.T) {
	id := BackupTempIdentity{VolumeSerial: 7}
	initial := backupDBObservation{id: id, name: `C:\work\copy.sqlite`, size: 3}
	fault := errors.New("injected failure")
	for _, tc := range []struct {
		name         string
		before       func(*backupDBObservation)
		after        func(*backupDBObservation)
		observeError int
		reader       io.ReaderAt
		cause        error
	}{
		{name: "first observation", observeError: 1, cause: fault},
		{name: "last observation", observeError: 2, cause: fault},
		{name: "too large", before: func(o *backupDBObservation) { o.size = backupDBBaselineMaxBytes + 1 }},
		{name: "negative size", before: func(o *backupDBObservation) { o.size = -1 }},
		{name: "wrong ID", before: func(o *backupDBObservation) { o.id.VolumeSerial++ }},
		{name: "wrong name", before: func(o *backupDBObservation) { o.name += "other" }},
		{name: "size changed", after: func(o *backupDBObservation) { o.size++ }},
		{name: "ID changed", after: func(o *backupDBObservation) { o.id.FileID[0]++ }},
		{name: "name changed", after: func(o *backupDBObservation) { o.name += "other" }},
		{name: "write time changed", after: func(o *backupDBObservation) { o.modified.LowDateTime++ }},
		{name: "short read", reader: strings.NewReader("a"), cause: io.ErrUnexpectedEOF},
		{name: "read failure", reader: baselineErrorReader{fault}, cause: fault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			observe := func() (backupDBObservation, error) {
				count++
				if count == tc.observeError {
					return backupDBObservation{}, fault
				}
				o := initial
				if tc.before != nil {
					tc.before(&o)
				}
				if count == 2 && tc.after != nil {
					tc.after(&o)
				}
				return o, nil
			}
			reader := tc.reader
			if reader == nil {
				reader = strings.NewReader("abc")
			}
			got, err := readBackupDBBaseline(reader, id, initial.name, observe)
			if got != (backupDBBaseline{}) || !errors.Is(err, errBackupDBBaseline) || (tc.cause != nil && !errors.Is(err, tc.cause)) {
				t.Fatal(got, err)
			}
		})
	}
}
