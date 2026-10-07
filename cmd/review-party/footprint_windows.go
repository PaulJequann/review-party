//go:build windows

package main

import "io/fs"

// ownedByCurrentUser is always true on Windows, where the host temp
// directory is per user.
func ownedByCurrentUser(fs.DirEntry) bool { return true }
