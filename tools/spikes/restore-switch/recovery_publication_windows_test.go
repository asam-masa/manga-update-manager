package restoreswitch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
)

func canonicalRecoveryTestDirectory(t *testing.T, dir string) string {
	t.Helper()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var name [32768]uint16
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &name[0], uint32(len(name)), 0)
	if err := errors.Join(err, f.Close()); err != nil || n == 0 || n >= uint32(len(name)) {
		t.Fatal(n, err)
	}
	return strings.TrimPrefix(windows.UTF16ToString(name[:n]), `\\?\`)
}

func prepareRecoveryRecord(t *testing.T, root, name string, body []byte) *platform.BackupTemp {
	t.Helper()
	temp, err := platform.CreateBackupTemp(context.Background(), filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := temp.Close(); err != nil {
			t.Error(err)
		}
	})
	if n, err := temp.Write(body); err != nil || n != len(body) {
		t.Fatal(n, err)
	}
	if err := temp.Sync(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(io.NewSectionReader(temp, 0, int64(len(body))+1))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("record readback mismatch", err)
	}
	return temp
}

func publishRecoveryRecord(t *testing.T, temp *platform.BackupTemp) {
	t.Helper()
	result, err := temp.PublishNoReplace(context.Background())
	if err != nil || result.State != platform.BackupPublishConfirmed {
		t.Fatal(result, err)
	}
	if err := errors.Join(temp.VerifyIdentity(), temp.Sync(), temp.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRecordsPublishSeparately(t *testing.T) {
	root := canonicalRecoveryTestDirectory(t, recoveryFixture(t, 0))
	if err := os.Mkdir(filepath.Join(root, "records"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := json.Marshal(recoveryFixturePlan)
	if err != nil {
		t.Fatal(err)
	}
	publishRecoveryRecord(t, prepareRecoveryRecord(t, root, planName, plan))
	content(t, root, planName, string(plan))
	move(t, root, liveDB, retiredDB)
	move(t, root, liveImage, retiredImage)
	move(t, root, stagedImage, liveImage)
	write(t, root, stagedDB, "updated-db")
	move(t, root, stagedDB, liveDB)
	if outcome, _, err := decideRecovery(root); outcome != "restore old" || err != nil {
		t.Fatal(outcome, err)
	}

	done, err := json.Marshal(recoveryCompletion{1, recoveryFixturePlan.ID, sha256.Sum256(plan)})
	if err != nil {
		t.Fatal(err)
	}
	publishRecoveryRecord(t, prepareRecoveryRecord(t, root, completeName, done))
	content(t, root, planName, string(plan))
	content(t, root, completeName, string(done))
	if outcome, moves, err := decideRecovery(root); outcome != "keep new" || len(moves) != 0 || err != nil {
		t.Fatal(outcome, moves, err)
	}

	// A second record cannot replace the published evidence. Its own
	// temporary content is retained so callers can classify the outcome.
	replacement := []byte("synthetic conflicting record")
	for _, entry := range []struct {
		name     string
		original []byte
	}{{planName, plan}, {completeName, done}} {
		temp := prepareRecoveryRecord(t, root, entry.name, replacement)
		result, err := temp.PublishNoReplace(context.Background())
		if err == nil || result.State != platform.BackupPublishCollision {
			t.Fatal(result, err)
		}
		if err := temp.Close(); err != nil {
			t.Fatal(err)
		}
		content(t, root, entry.name, string(entry.original))
		content(t, filepath.Dir(temp.Path()), filepath.Base(temp.Path()), string(replacement))
	}
	if _, moves, err := decideRecovery(root); len(moves) != 0 || !errors.Is(err, errRecoveryStop) {
		t.Fatal(moves, err)
	}
}

func TestUnpublishedNativeRecoveryRecordStopsDecision(t *testing.T) {
	root := canonicalRecoveryTestDirectory(t, recoveryFixture(t, 0))
	if err := os.Mkdir(filepath.Join(root, "records"), 0700); err != nil {
		t.Fatal(err)
	}
	temp := prepareRecoveryRecord(t, root, planName, []byte("unpublished record"))
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if _, moves, err := decideRecovery(root); len(moves) != 0 || !errors.Is(err, errRecoveryStop) {
		t.Fatal(moves, err)
	}
	content(t, filepath.Dir(temp.Path()), filepath.Base(temp.Path()), "unpublished record")
}
