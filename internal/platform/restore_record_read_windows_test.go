package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/restore"
)

func TestReadRestoreRecordBody(t *testing.T) {
	for _, record := range []struct {
		name  string
		limit int
	}{
		{restore.PlanName, restore.MaxPlanBytes},
		{restore.CompletionName, restore.MaxCompletionBytes},
	} {
		t.Run(record.name, func(t *testing.T) {
			for _, test := range []struct {
				label string
				size  int
				err   error
			}{
				{"empty", 0, nil},
				{"exact limit", record.limit, nil},
				{"oversize", record.limit + 1, errRestoreRecordTooLarge},
			} {
				t.Run(test.label, func(t *testing.T) {
					dir := t.TempDir()
					input := bytes.Repeat([]byte{0x61, 0x62}, (test.size+1)/2)[:test.size]
					if err := os.WriteFile(filepath.Join(dir, record.name), input, 0600); err != nil {
						t.Fatal(err)
					}
					parent := openRestoreTestDirectory(t, dir)
					file := requireRestoreTestChild(t, parent, record.name)
					for range 2 {
						body, err := readRestoreRecordBody(context.Background(), file, record.name)
						if !errors.Is(err, test.err) || test.err == nil && !bytes.Equal(body, input) ||
							test.err != nil && body != nil {
							t.Fatalf("body size=%d err=%v", len(body), err)
						}
					}
				})
			}
		})
	}
}

func TestReadBoundedRestoreRecordDiscardsPartialBody(t *testing.T) {
	readFailure := errors.New("synthetic read failure")
	for _, test := range []struct {
		label  string
		err    error
		cancel bool
		want   error
	}{
		{"partial read error", readFailure, false, readFailure},
		{"EOF with another cause", errors.Join(io.EOF, readFailure), false, readFailure},
		{"cancel at EOF", io.EOF, true, context.Canceled},
	} {
		t.Run(test.label, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			body, err := readBoundedRestoreRecord(ctx, 2, func(p []byte) (int, error) {
				calls++
				p[0] = 'x'
				if calls == 1 {
					return 1, nil
				}
				if len(p) != 2 {
					t.Fatalf("second request=%d, want remaining limit plus one", len(p))
				}
				if test.cancel {
					cancel()
				}
				return 1, test.err
			})
			if body != nil || calls != 2 || !errors.Is(err, test.want) {
				t.Fatalf("body=%q calls=%d err=%v", body, calls, err)
			}
		})
	}
}
