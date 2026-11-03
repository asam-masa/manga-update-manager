package platform

import (
	"errors"
	"testing"
)

func TestParseBackupDestinationPreservesOrdinaryNames(t *testing.T) {
	for _, tc := range []struct{ path, directory, name string }{
		{`C:\backup.zip`, `C:\`, `backup.zip`},
		{`H:\漫画 保存\退避 2026.zip`, `H:\漫画 保存`, `退避 2026.zip`},
		{`d:\folder\保存.ZIP`, `d:\folder`, `保存.ZIP`},
		{`D:\ leading\ backup.zip`, `D:\ leading`, ` backup.zip`},
		{`C:\COM10\console.zip`, `C:\COM10`, `console.zip`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got, err := ParseBackupDestination(tc.path)
			if err != nil || got.Directory != tc.directory || got.Name != tc.name {
				t.Fatalf("got %#v, %v; want %q / %q", got, err, tc.directory, tc.name)
			}
		})
	}
}

func TestParseBackupDestinationRejectsAmbiguousOrInvalidNames(t *testing.T) {
	for _, path := range []string{
		"", `backup.zip`, `C:backup.zip`, `\backup.zip`, `1:\backup.zip`,
		`\\server\share\backup.zip`, `\\?\C:\backup.zip`, `\\.\C:\backup.zip`,
		`C:\`, `C:\folder\`, `C:\folder\.\backup.zip`, `C:\folder\..\backup.zip`,
		`C:\folder\\backup.zip`, `C:/folder/backup.zip`, `C:\folder/backup.zip`,
		`C:\folder.\backup.zip`, `C:\folder \backup.zip`, `C:\backup.zip.`, `C:\backup.zip `,
		`C:\backup.zip:stream`, `C:\folder:stream\backup.zip`, `C:\bad?\backup.zip`,
		`C:\bad<name.zip`, `C:\bad>name.zip`, `C:\bad"name.zip`, `C:\bad|name.zip`, `C:\bad*name.zip`,
		`C:\CON`, `C:\nul.zip`, `C:\COM1\backup.zip`, `C:\LPT9.zip`, `C:\CONOUT$.zip`,
		`C:\NUL .zip`, `C:\COM¹.zip`, `C:\lpt²\backup.zip`, `C:\aux.txt`,
		"C:\\bad\x00.zip", "C:\\bad\n.zip", "C:\\bad\t\\backup.zip", "C:\\bad\x1f.zip", "C:\\bad\xff.zip",
	} {
		t.Run(path, func(t *testing.T) {
			got, err := ParseBackupDestination(path)
			if !errors.Is(err, ErrInvalidBackupDestination) || got != (BackupDestination{}) {
				t.Fatalf("accepted or returned partial destination: %#v, %v", got, err)
			}
		})
	}
}
