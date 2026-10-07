//go:build unix

package hostrun

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func defaultRoot() string {
	return filepath.Join(hostTemp, "review-party-runtime-"+strconv.Itoa(os.Getuid()))
}

const removeAttempts = 1

func validateRoot(root string) error {
	info, err := lstatOrCreate(root)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("runtime root %s is a symlink", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime root %s is not a directory", root)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("runtime root %s is owned by uid %d, not %d", root, stat.Uid, os.Getuid())
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("runtime root %s has mode %o; it must be 0700", root, info.Mode().Perm())
	}
	return nil
}
