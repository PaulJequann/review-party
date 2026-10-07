package hostrun

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	Init()
	if mode := os.Getenv(helperVar); mode != "" {
		os.Exit(runHelper(mode))
	}
	os.Exit(m.Run())
}

const (
	helperVar = "REVIEW_PARTY_HOSTRUN_HELPER"
	rootVar   = "REVIEW_PARTY_HOSTRUN_ROOT"
	pidsVar   = "REVIEW_PARTY_HOSTRUN_PIDS"
	readyVar  = "REVIEW_PARTY_HOSTRUN_READY"
	codeVar   = "REVIEW_PARTY_HOSTRUN_CODE"
	holdVar   = "REVIEW_PARTY_HOSTRUN_HOLD"
)

var helpers = map[string]func() int{
	"sleep": block,
	"stubborn": func() int {
		// Catch rather than Ignore: on Windows an ignored Ctrl+Break falls
		// through to the default handler, which exits the process.
		signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM, os.Interrupt)
		announce("")
		return block()
	},
	"exit": func() int {
		code, err := strconv.Atoi(os.Getenv(codeVar))
		if err != nil {
			panic(err)
		}
		return code
	},
	"env": func() int {
		for _, name := range tempVariables {
			fmt.Println(os.Getenv(name))
		}
		return 0
	},
	"tree": func() int {
		spawnTree()
		return block()
	},
	"tree-exit": func() int {
		spawnTree()
		return 0
	},
	"owner": runOwner,
	"reaper": func() int {
		r, err := Open(Options{Root: os.Getenv(rootVar), Warn: warnStderr})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		r.Close()
		return 0
	},
	"hold": func() int {
		file, err := os.Open(os.Getenv(holdVar))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer file.Close() //nolint:errcheck // The helper holds the file until it is killed.
		announce("")
		return block()
	},
}

func runHelper(mode string) int {
	helper, ok := helpers[mode]
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown helper", mode)
		return 1
	}
	return helper()
}

// block parks the helper on a timer rather than an empty select, which the
// runtime would report as a deadlock.
func block() int {
	for {
		time.Sleep(time.Hour)
	}
}

func warnStderr(line string) { fmt.Fprintln(os.Stderr, line) }

func announce(content string) {
	if path := os.Getenv(readyVar); path != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			panic(err)
		}
	}
}

func spawnTree() {
	pids := []int{os.Getpid()}
	plain := helperCommand("sleep")
	if err := plain.Start(); err != nil {
		panic(err)
	}
	pids = append(pids, plain.Process.Pid)
	if escapee := escapeeCommand("sleep"); escapee != nil {
		if err := escapee.Start(); err != nil {
			panic(err)
		}
		pids = append(pids, escapee.Process.Pid)
	}
	var text strings.Builder
	for _, pid := range pids {
		fmt.Fprintln(&text, pid)
	}
	if err := os.WriteFile(os.Getenv(pidsVar), []byte(text.String()), 0o600); err != nil {
		panic(err)
	}
}

func runOwner() int {
	r, err := Open(Options{Root: os.Getenv(rootVar), Warn: warnStderr, Command: "owner"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	p, err := r.Start(helperCommand("tree", pidsVar+"="+os.Getenv(pidsVar)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for {
		if data, err := os.ReadFile(os.Getenv(pidsVar)); err == nil && bytes.HasSuffix(data, []byte("\n")) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	announce(fmt.Sprintf("%s\n%d\n", r.dir, p.sentinel.Process.Pid))
	block()
	return 0
}

// Later pairs in extra override earlier environment values.
func helperCommand(mode string, extra ...string) *exec.Cmd {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	cmd := exec.Command(executable)
	cmd.Env = append(os.Environ(), helperVar+"="+mode)
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}

func startHelper(t *testing.T, r *Run, cmd *exec.Cmd) *Process {
	t.Helper()
	p, err := r.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Stop(closeStopGrace) }) //nolint:errcheck // Test cleanup is best effort.
	return p
}

func startChild(t *testing.T, cmd *exec.Cmd) *exec.Cmd {
	t.Helper()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //nolint:errcheck // Test cleanup is best effort.
		_ = cmd.Wait()         //nolint:errcheck // Same.
	})
	return cmd
}

func killLater(t *testing.T, pids []int) {
	t.Helper()
	t.Cleanup(func() {
		for _, pid := range pids {
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill() //nolint:errcheck // Test cleanup is best effort.
			}
		}
	})
}

func readPIDs(t *testing.T, path string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("pids file line %q: %v", line, err)
		}
		pids = append(pids, pid)
	}
	return pids
}

func waitFor(t *testing.T, timeout time.Duration, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func running(pid int) bool {
	_, err := identify(pid)
	return err == nil
}

func survivors(pids []int) []int {
	var alive []int
	for _, pid := range pids {
		if running(pid) {
			alive = append(alive, pid)
		}
	}
	return alive
}

func rootNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func testRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "root")
}

func openRun(t *testing.T, root string, warnings *[]string) *Run {
	t.Helper()
	r, err := Open(Options{Root: root, Command: "test", Version: "test", Warn: func(line string) {
		if warnings != nil {
			*warnings = append(*warnings, line)
		}
		t.Log(line)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func fakeDeadRun(t *testing.T, root, id string, records ...ProcessRecord) string {
	t.Helper()
	dir := filepath.Join(root, id)
	for _, sub := range []string{tempName, filepath.Join(viewsName, "1"), procsName} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	owner := OwnerRecord{Schema: recordSchema, ID: id, Created: fakeTime, Command: "fake"}
	writeJSON(t, filepath.Join(dir, ownerName), owner)
	for i, record := range records {
		writeJSON(t, filepath.Join(dir, procsName, strconv.Itoa(1000+i)+".json"), record)
	}
	for _, name := range []string{leaseName, filepath.Join(tempName, "scratch.txt"), filepath.Join(viewsName, "1", "file.txt")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var fakeTime = time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("want *exec.ExitError, got %v", err)
	}
	return exitErr.ExitCode()
}
