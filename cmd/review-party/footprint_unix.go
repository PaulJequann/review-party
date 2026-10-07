//go:build unix

package main

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByCurrentUser keeps another user's entries in a shared temp
// directory out of this user's footprint.
func ownedByCurrentUser(entry fs.DirEntry) bool {
	info, err := entry.Info()
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
