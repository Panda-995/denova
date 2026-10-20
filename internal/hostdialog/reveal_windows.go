package hostdialog

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	revealShell32              = windows.NewLazySystemDLL("shell32.dll")
	shParseDisplayName         = revealShell32.NewProc("SHParseDisplayName")
	shOpenFolderAndSelectItems = revealShell32.NewProc("SHOpenFolderAndSelectItems")
)

func revealPath(ctx context.Context, path string, _ os.FileInfo) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode shell path: %w", err)
	}
	// Shell calls and COM cleanup must stay on the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	switch err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	case syscall.Errno(windows.RPC_E_CHANGED_MODE):
		// Another caller owns this thread's COM apartment.
	default:
		return fmt.Errorf("initialize file manager shell: %w", err)
	}

	var item unsafe.Pointer
	hr, _, _ := shParseDisplayName.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&item)), 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("parse shell path (HRESULT 0x%08x): %w", uint32(hr), syscall.Errno(uint32(hr)))
	}
	defer windows.CoTaskMemFree(item)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Explorer can return exit code 1 even after revealing an item successfully.
	// Use the Shell API's result instead. A zero item count selects this absolute
	// item in its parent folder, for both files and directories.
	hr, _, _ = shOpenFolderAndSelectItems.Call(uintptr(item), 0, 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("select shell item (HRESULT 0x%08x): %w", uint32(hr), syscall.Errno(uint32(hr)))
	}
	return nil
}
