package platform

import (
	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
	"runtime"
)

// OpenURL asks the registered browser to open a URL, without a command shell.
// ShellExecute's success means acceptance of the request, not page loading.
func OpenURL(url string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED|ole.COINIT_DISABLE_OLE1DDE); err != nil {
		// S_FALSE also acquires a COM initialization reference that must be released.
		if e, ok := err.(*ole.OleError); !ok || e.Code() != 1 {
			return err
		}
	}
	defer ole.CoUninitialize()
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, target, nil, nil, windows.SW_SHOWNORMAL)
}
