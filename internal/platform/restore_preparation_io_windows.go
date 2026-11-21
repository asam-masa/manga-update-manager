package platform

import "os"

// I/O uses the creation handle only. The caller retains parents and owns byte
// limits, cancellation between operations, and content validation. No operation
// here authorizes deletion, restore, or publication, and none reopens Path.
func (f *RestorePreparationFile) Write(p []byte) (int, error) {
	if f == nil {
		return 0, os.ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.release.CloseAttempted || f.discardAttempted || f.file == nil {
		return 0, os.ErrClosed
	}
	return f.file.Write(p)
}

// ReadAt preserves the sequential write position and the OS count/error pair.
func (f *RestorePreparationFile) ReadAt(p []byte, offset int64) (int, error) {
	if f == nil {
		return 0, os.ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.release.CloseAttempted || f.discardAttempted || f.file == nil {
		return 0, os.ErrClosed
	}
	return f.file.ReadAt(p, offset)
}

// Sync success alone is not proof of whole-backup durability or validity.
func (f *RestorePreparationFile) Sync() error {
	if f == nil {
		return os.ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.release.CloseAttempted || f.discardAttempted || f.file == nil {
		return os.ErrClosed
	}
	return f.file.Sync()
}

func (f *RestorePreparationFile) Size() (int64, error) {
	if f == nil {
		return 0, os.ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.release.CloseAttempted || f.discardAttempted || f.file == nil {
		return 0, os.ErrClosed
	}
	info, err := f.file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
