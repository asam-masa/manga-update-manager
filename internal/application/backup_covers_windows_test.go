package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type inventoryCloseFunc func() error

func (f inventoryCloseFunc) Close() error { return f() }

func inventoryRecord(id int, size int64) storage.ManagedCover {
	r := backupCoverRecord(nil)
	r.ID = fmt.Sprintf("%032x", id)
	r.RelativePath = "covers/" + r.ID + ".png"
	r.Info.ByteSize = size
	return r
}

func inventoryImage(r storage.ManagedCover) BackupCoverInspection {
	return BackupCoverInspection{Image: &backup.Image{ID: r.ID, Path: r.RelativePath, Size: r.Info.ByteSize, SHA256: hex.EncodeToString(r.Info.SHA256[:])}}
}

func inventoryHold(context.Context, string) (io.Closer, error) {
	return inventoryCloseFunc(func() error { return nil }), nil
}

func TestInspectBackupCoversBoundsAndOrdering(t *testing.T) {
	for _, mode := range []string{"empty", "mixed", "count-limit", "count-over", "duplicate", "total-limit", "total-over"} {
		t.Run(mode, func(t *testing.T) {
			var records []storage.ManagedCover
			switch mode {
			case "mixed":
				records = []storage.ManagedCover{inventoryRecord(3, 1), inventoryRecord(1, 1), inventoryRecord(2, 1)}
			case "count-limit", "count-over":
				n := backup.MaxImages
				if mode == "count-over" {
					n++
				}
				for i := 1; i <= n; i++ {
					records = append(records, inventoryRecord(i, 1))
				}
			case "duplicate":
				records = []storage.ManagedCover{inventoryRecord(1, 1), inventoryRecord(2, 1), inventoryRecord(1, 1)}
			case "total-limit", "total-over":
				for remain := backup.MaxTotalBytes; remain > 0; {
					n := min(remain, backup.MaxImageBytes)
					records = append(records, inventoryRecord(len(records)+1, n))
					remain -= n
				}
				if mode == "total-over" {
					records = append(records, inventoryRecord(len(records)+1, 1))
				}
			}
			before := append([]storage.ManagedCover(nil), records...)
			calls, holds, closes := 0, 0, 0
			got, err := inspectBackupCovers(context.Background(), "synthetic", records, func(context.Context, string) (io.Closer, error) {
				holds++
				return inventoryCloseFunc(func() error { closes++; return nil }), nil
			}, func(_ context.Context, _ string, r storage.ManagedCover) (BackupCoverInspection, error) {
				calls++
				if mode == "count-limit" || mode == "mixed" && r.ID != inventoryRecord(1, 1).ID {
					reason := "missing"
					if r.ID == inventoryRecord(3, 1).ID {
						reason = "corrupt"
					}
					return BackupCoverInspection{Omitted: &backup.OmittedImage{ID: r.ID, Path: r.RelativePath, Reason: reason}}, nil
				}
				return inventoryImage(r), nil
			})
			if !reflect.DeepEqual(records, before) {
				t.Fatal("input order changed")
			}
			reject := mode == "count-over" || mode == "duplicate" || mode == "total-over"
			if reject {
				if !errors.Is(err, ErrBackupCovers) || got.Images != nil || got.OmittedImages != nil {
					t.Fatal(got, err)
				}
				if mode != "total-over" && (holds != 0 || calls != 0) {
					t.Fatal("invalid input reached I/O", holds, calls)
				}
			} else {
				if err != nil || got.Images == nil || got.OmittedImages == nil || len(got.Images)+len(got.OmittedImages) != len(records) {
					t.Fatal(got, err)
				}
				if mode == "mixed" && (len(got.Images) != 1 || got.Images[0].ID != records[1].ID || got.OmittedImages[0].ID != records[2].ID || got.OmittedImages[1].ID != records[0].ID) {
					t.Fatal(got)
				}
			}
			if closes != holds {
				t.Fatal("release mismatch", holds, closes)
			}
		})
	}
}

func TestInspectBackupCoversNoPartialResults(t *testing.T) {
	fault, closeFault := errors.New("inspection failed"), errors.Join(platform.ErrCoverRelease, errors.New("release failed"))
	for _, mode := range []string{"hold", "inspect", "close", "late-cancel", "mid-cancel", "both", "neither", "id", "reason", "size", "hash"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, closes := 0, 0
			got, err := inspectBackupCovers(ctx, "synthetic", []storage.ManagedCover{inventoryRecord(1, 1), inventoryRecord(2, 1)}, func(context.Context, string) (io.Closer, error) {
				if mode == "hold" {
					return nil, fault
				}
				return inventoryCloseFunc(func() error {
					closes++
					if mode == "late-cancel" {
						cancel()
					}
					if mode == "inspect" || mode == "close" {
						return closeFault
					}
					return nil
				}), nil
			}, func(_ context.Context, _ string, r storage.ManagedCover) (BackupCoverInspection, error) {
				calls++
				out := inventoryImage(r)
				if calls == 1 {
					return out, nil
				}
				switch mode {
				case "inspect":
					return out, fault
				case "mid-cancel":
					cancel()
				case "both":
					out.Omitted = &backup.OmittedImage{}
				case "neither":
					out = BackupCoverInspection{}
				case "id":
					out.Image.ID = "other"
				case "reason":
					out.Image = nil
					out.Omitted = &backup.OmittedImage{ID: r.ID, Path: r.RelativePath, Reason: "unknown"}
				case "size":
					out.Image.Size++
				case "hash":
					out.Image.SHA256 = "wrong"
				}
				return out, nil
			})
			if !errors.Is(err, ErrBackupCovers) || got.Images != nil || got.OmittedImages != nil {
				t.Fatal("partial result", got, err)
			}
			want := 1
			if mode == "hold" {
				want = 0
			}
			if closes != want {
				t.Fatal(closes)
			}
			if (mode == "hold" || mode == "inspect") && !errors.Is(err, fault) {
				t.Fatal(err)
			}
			if (mode == "inspect" || mode == "close") && !errors.Is(err, closeFault) {
				t.Fatal("release cause lost", err)
			}
			if (mode == "mid-cancel" || mode == "late-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := inspectBackupCovers(ctx, "synthetic", nil, func(context.Context, string) (io.Closer, error) { t.Fatal("cancelled I/O"); return nil, nil }, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestInspectBackupCoversRealFilesAndEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	var records []storage.ManagedCover
	for i := 1; i <= 3; i++ {
		data := backupCoverPNG(t, 3)
		if i == 3 {
			data = []byte("invalid PNG")
		}
		r := inventoryRecord(i, int64(len(data)))
		info, err := platform.WriteNewCover(context.Background(), dir, r.ID, data)
		if err != nil {
			t.Fatal(err)
		}
		r.Info = &storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
		records = append(records, r)
		if i == 2 {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(r.RelativePath))); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := InspectBackupCovers(context.Background(), dir, records)
	if err != nil || len(got.Images) != 1 || len(got.OmittedImages) != 2 || got.OmittedImages[0].Reason != "missing" || got.OmittedImages[1].Reason != "corrupt" {
		t.Fatal(got, err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal("parent leaked", err)
	}
	if err := os.Rename(dir+"-moved", dir); err != nil {
		t.Fatal(err)
	}
	got, err = InspectBackupCovers(context.Background(), dir, nil)
	if err != nil || got.Images == nil || got.OmittedImages == nil || len(got.Images)+len(got.OmittedImages) != 0 {
		t.Fatal(got, err)
	}
	absent := t.TempDir()
	got, err = InspectBackupCovers(context.Background(), absent, nil)
	if err == nil || got.Images != nil || got.OmittedImages != nil {
		t.Fatal("missing folder ignored", got, err)
	}
	if _, err := os.Stat(filepath.Join(absent, "covers")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("folder created", err)
	}
}
