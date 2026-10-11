package platform

import "golang.org/x/sys/windows"

// ShowStartupError works before Wails creates a window; no shell is involved.
func ShowStartupError(message string) error {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return err
	}
	title, _ := windows.UTF16PtrFromString("Manga Update Manager")
	_, err = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONWARNING)
	return err
}
