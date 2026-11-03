package platform

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var ErrInvalidBackupDestination = errors.New("unsupported backup destination path")

// BackupDestination is a syntactically checked path, not an opened or trusted
// destination. Existence, local NTFS, reparse points, identity and capabilities
// must be checked separately while retaining directory handles before any write.
type BackupDestination struct {
	Directory string
	Name      string
}

// ParseBackupDestination accepts an ordinary drive-letter absolute file path.
// It performs no I/O and never repairs or resolves the supplied spelling.
func ParseBackupDestination(name string) (BackupDestination, error) {
	invalid := func() (BackupDestination, error) { return BackupDestination{}, ErrInvalidBackupDestination }
	volume := filepath.VolumeName(name)
	if !utf8.ValidString(name) || len(volume) != 2 || volume[1] != ':' ||
		!((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z')) ||
		!filepath.IsAbs(name) || strings.ContainsAny(name, "/\x00") || filepath.Clean(name) != name || len(name) <= 3 {
		return invalid()
	}
	for _, component := range strings.Split(name[3:], `\`) {
		if component == "" || component == "." || component == ".." || backupReservedName(component) || strings.ContainsAny(component, `:<>"|?*`) ||
			strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return invalid()
		}
		for _, r := range component {
			if r < 32 {
				return invalid()
			}
		}
	}
	return BackupDestination{Directory: filepath.Dir(name), Name: filepath.Base(name)}, nil
}

// Reject DOS device names lexically, including extension-bearing spellings.
// Do not rely on IsLocal's environment-dependent device-name lookup.
func backupReservedName(component string) bool {
	stem, _, _ := strings.Cut(component, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if strings.HasPrefix(stem, prefix) {
			digit := strings.TrimPrefix(stem, prefix)
			if len(digit) == 1 && digit[0] >= '1' && digit[0] <= '9' || digit == "¹" || digit == "²" || digit == "³" {
				return true
			}
		}
	}
	return false
}
