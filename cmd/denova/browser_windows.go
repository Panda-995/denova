package main

import (
	"fmt"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

// Use the registered URL handler directly. Shell lookup through PATH/PATHEXT
// can fail on otherwise usable Windows installations and after relaunches.
func launchBrowser(url string) error {
	file, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	// URL associations can use COM shell extensions; initialize and release COM
	// on the same OS thread, including when the browser was already running.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	switch err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	case syscall.Errno(windows.RPC_E_CHANGED_MODE):
		// Another caller owns this thread's COM apartment.
	default:
		return fmt.Errorf("initialize browser shell: %w", err)
	}
	return windows.ShellExecute(0, nil, file, nil, nil, windows.SW_SHOWNORMAL)
}
