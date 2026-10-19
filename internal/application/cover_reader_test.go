package application

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type coverReadRepositoryStub struct {
	work   func(context.Context, int64) (manga.Work, error)
	record func(context.Context, string) (storage.ManagedCover, error)
}

func (s coverReadRepositoryStub) WorkByID(ctx context.Context, id int64) (manga.Work, error) {
	return s.work(ctx, id)
}
func (s coverReadRepositoryStub) ManagedCoverByID(ctx context.Context, id string) (storage.ManagedCover, error) {
	return s.record(ctx, id)
}

func TestCoverReaderCurrentReferenceAndPreservation(t *testing.T) {
	a, s, work, dir := importFixture(t)
	first, err := a.Select(context.Background(), work.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewCoverReader(s, a.operations, dir)
	data, err := reader.Read(context.Background(), work.ID)
	want, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(first.Work.ThumbnailPath)))
	if err != nil || readErr != nil || !bytes.Equal(data, want) {
		t.Fatal("first read", err, readErr)
	}
	second, err := a.Select(context.Background(), work.ID)
	if err != nil || second.Work.ThumbnailPath == first.Work.ThumbnailPath {
		t.Fatal("replacement", err)
	}
	if _, err := reader.Read(context.Background(), work.ID); err != nil {
		t.Fatal("current read", err)
	}
	stored, err := s.WorkByID(context.Background(), work.ID)
	if err != nil || !reflect.DeepEqual(stored, *second.Work) {
		t.Fatal("read changed work", err)
	}
	if _, err := a.Remove(context.Background(), work.ID); err != nil {
		t.Fatal(err)
	}
	if data, err := reader.Read(context.Background(), work.ID); data != nil || !errors.Is(err, ErrCoverUnavailable) {
		t.Fatal("unset returned data", err)
	}
}

func TestCoverReaderRejectsWithoutChangingReference(t *testing.T) {
	for _, mode := range []string{"legacy", "reserved", "record-missing", "missing", "changed", "broken", "record-error", "db-stopped", "release", "unsafe", "stop-after-read", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, s, work, dir := importFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result, err := a.Select(ctx, work.ID)
			if err != nil {
				t.Fatal(err)
			}
			before := *result.Work
			reader := NewCoverReader(s, a.operations, dir)
			name := filepath.Join(dir, filepath.FromSlash(before.ThumbnailPath))
			id := strings.TrimSuffix(strings.TrimPrefix(before.ThumbnailPath, "covers/"), ".png")
			switch mode {
			case "legacy":
				reader.repository = coverReadRepositoryStub{work: func(context.Context, int64) (manga.Work, error) { return work, nil }}
			case "reserved", "record-missing", "record-error", "db-stopped":
				reader.repository = coverReadRepositoryStub{work: s.WorkByID, record: func(context.Context, string) (storage.ManagedCover, error) {
					if mode == "reserved" {
						return storage.ManagedCover{ID: id, RelativePath: before.ThumbnailPath, State: "reserved"}, nil
					}
					if mode == "record-missing" {
						return storage.ManagedCover{}, storage.ErrManagedCoverNotFound
					}
					if mode == "db-stopped" {
						return storage.ManagedCover{}, storage.ErrDBStopped
					}
					return storage.ManagedCover{}, errors.New("synthetic database error")
				}}
			case "missing":
				if err := os.Rename(name, name+"-retained"); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "broken":
				// Even a valid OS/record match is insufficient without PNG validation.
				reader.read = func(context.Context, string, string, platform.CoverFileInfo) ([]byte, error) {
					return []byte("broken"), nil
				}
			case "release", "unsafe", "stop-after-read":
				read := reader.read
				reader.read = func(ctx context.Context, dir, id string, info platform.CoverFileInfo) ([]byte, error) {
					data, err := read(ctx, dir, id, info)
					if err != nil {
						return data, err
					}
					if len(a.operations.references) != 1 {
						t.Error("reference gate released before read")
					}
					if mode == "release" {
						return data, platform.ErrCoverRelease
					}
					if mode == "unsafe" {
						return data, platform.ErrUnsafeCoverDirectory
					}
					a.operations.Stop()
					return data, nil
				}
			case "cancel":
				cancel()
			}
			if data, err := reader.Read(ctx, work.ID); data != nil || err == nil {
				t.Fatal("rejected read returned bytes", err)
			}
			stored, err := s.WorkByID(context.Background(), work.ID)
			if err != nil || !reflect.DeepEqual(stored, before) {
				t.Fatal("work changed", err)
			}
			if mode == "release" || mode == "unsafe" || mode == "db-stopped" || mode == "stop-after-read" {
				if _, err := a.operations.BeginRead(); !errors.Is(err, ErrCoverStopped) {
					t.Fatal("not stopped", err)
				}
			}
			unlock, err := a.operations.LockReferences(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			unlock()
		})
	}
}

func TestCoverReaderCloseWaitsAndStopsAdmission(t *testing.T) {
	a, s, work, dir := importFixture(t)
	if _, err := a.Select(context.Background(), work.ID); err != nil {
		t.Fatal(err)
	}
	reader := NewCoverReader(s, a.operations, dir)
	read := reader.read
	entered, release := make(chan struct{}), make(chan struct{})
	reader.read = func(ctx context.Context, dir, id string, info platform.CoverFileInfo) ([]byte, error) {
		close(entered)
		<-release
		return read(ctx, dir, id, info)
	}
	finished := make(chan error, 1)
	go func() { _, err := reader.Read(context.Background(), work.ID); finished <- err }()
	<-entered
	drained, err := a.operations.RequestClose()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
		t.Fatal("closed before release")
	default:
	}
	if _, err := reader.Read(context.Background(), work.ID); !errors.Is(err, ErrCoverClosing) {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal("close cancelled admitted read", err)
	}
	<-drained
}

func TestCoverReaderStopsBeforeNextDBStage(t *testing.T) {
	var operations CoverOperations
	repository := coverReadRepositoryStub{
		work: func(context.Context, int64) (manga.Work, error) {
			operations.Stop()
			return manga.Work{ThumbnailPath: "covers/" + strings.Repeat("a", 32) + ".png"}, nil
		},
		record: func(context.Context, string) (storage.ManagedCover, error) {
			t.Fatal("DB lookup after Stop")
			return storage.ManagedCover{}, nil
		},
	}
	reader := NewCoverReader(repository, &operations, "unused")
	if data, err := reader.Read(context.Background(), 1); data != nil || !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
	if data, err := reader.Read(context.Background(), 0); data != nil || !errors.Is(err, ErrCoverOperationInvalid) {
		t.Fatal(err)
	}
	if data, err := reader.Read(context.Background(), 1); data != nil || !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
}
