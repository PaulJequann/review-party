package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reviewparty/internal/hostrun"
)

func TestMain(m *testing.M) {
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		panic(err)
	}
	home, err := os.MkdirTemp(tempRoot, "review-party-engine-")
	if err != nil {
		panic(err)
	}
	for key, value := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_STATE_HOME": filepath.Join(home, "state"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	hostrun.Init()
	status := m.Run()
	if err := os.RemoveAll(home); err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1
	}
	os.Exit(status)
}
