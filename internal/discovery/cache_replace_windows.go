//go:build windows

package discovery

import (
	"fmt"
	"syscall"
	"unsafe"
)

const moveFileReplaceExisting = 0x1

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceCacheFile(oldPath, newPath string) error {
	oldPathPointer, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPathPointer, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileEx.Call(
		uintptr(unsafe.Pointer(oldPathPointer)),
		uintptr(unsafe.Pointer(newPathPointer)),
		moveFileReplaceExisting,
	)
	if result == 0 {
		return fmt.Errorf("replace discovery cache: %w", callErr)
	}
	return nil
}
