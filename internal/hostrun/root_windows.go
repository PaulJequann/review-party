//go:build windows

package hostrun

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// DefaultRoot is the per-user runtime root under the host temp directory.
// %TEMP% is already per-user, so the root needs no uid suffix.
func DefaultRoot() string {
	return filepath.Join(hostTemp, "review-party-runtime")
}

// Open handles and process working directories refuse deletion for a while.
const removeAttempts = 6

func validateRoot(root string) error {
	info, err := lstatOrCreate(root)
	if err != nil {
		return err
	}
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return fmt.Errorf("runtime root %s is a reparse point", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime root %s is not a directory", root)
	}
	return nil
}
