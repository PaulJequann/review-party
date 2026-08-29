//go:build windows

package discovery

import "strings"

func environmentNameKey(name string) string { return strings.ToLower(name) }
