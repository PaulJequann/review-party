package engine

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reviewparty/internal/hostrun"
)

func TestMain(m *testing.M) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		panic(err)
	}
	home, err := os.MkdirTemp(tempRoot, "review-party-engine-")
	if err != nil {
		panic(err)
	}
	temp := filepath.Join(home, "tmp")
	if err := os.Mkdir(temp, 0o700); err != nil {
		panic(err)
	}
	for key, value := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_STATE_HOME": filepath.Join(home, "state"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
		"APPDATA": filepath.Join(home, "AppData", "Roaming"),
		"TMPDIR":  temp, "GOPATH": cmp.Or(os.Getenv("GOPATH"), filepath.Join(userHome, "go")),
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
