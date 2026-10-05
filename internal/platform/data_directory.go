package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDirectory resolves the OS application-data location and creates our directory.
// On Windows UserConfigDir uses %AppData%; no working-directory fallback is used.
func DataDirectory() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve app data: %w", err)
	}
	return prepareDataDirectory(root)
}

func prepareDataDirectory(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("app data root must be absolute")
	}
	directory := filepath.Join(root, "MangaUpdateManager")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", fmt.Errorf("create app data directory: %w", err)
	}
	return directory, nil
}
