//go:build !windows

package discovery

import "os"

func replaceCacheFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
