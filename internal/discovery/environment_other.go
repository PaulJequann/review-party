//go:build !windows

package discovery

func environmentNameKey(name string) string { return name }
