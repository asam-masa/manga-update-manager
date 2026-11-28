package platform

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/restore"
	"golang.org/x/sys/windows"
)

var errRestoreRecordTooLarge = errors.New("restore record exceeds its size limit")

// readRestoreRecordBody borrows the verified file handle and uses its file
// position exclusively, starting at zero. The caller owns the handle and
// must recheck the complete acquisition before adopting the bytes.
func readRestoreRecordBody(ctx context.Context, handle windows.Handle, name string) ([]byte, error) {
	var limit int
	switch name {
	case restore.PlanName:
		limit = restore.MaxPlanBytes
	case restore.CompletionName:
		limit = restore.MaxCompletionBytes
	default:
		return nil, fmt.Errorf("invalid restore record file name %q", name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := windows.Seek(handle, 0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek restore record %q: %w", name, err)
	}
	body, err := readBoundedRestoreRecord(ctx, limit, func(p []byte) (int, error) {
		var count uint32
		err := windows.ReadFile(handle, p, &count, nil)
		if err == windows.ERROR_HANDLE_EOF || err == nil && count == 0 {
			err = io.EOF
		}
		return int(count), err
	})
	if err != nil {
		return nil, fmt.Errorf("read restore record %q: %w", name, err)
	}
	return body, nil
}

func readBoundedRestoreRecord(ctx context.Context, limit int, read func([]byte) (int, error)) ([]byte, error) {
	var body []byte
	var chunk [32 * 1024]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		request := chunk[:min(len(chunk), limit+1-len(body))]
		n, err := read(request)
		if err != nil && err != io.EOF {
			return nil, errors.Join(err, ctx.Err())
		}
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, cancelErr
		}
		if n < 0 || n > len(request) {
			return nil, errors.New("invalid restore record read count")
		}
		if n > limit-len(body) {
			return nil, errRestoreRecordTooLarge
		}
		body = append(body, request[:n]...)
		if err == io.EOF {
			return body, nil
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
}
