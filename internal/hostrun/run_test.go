package hostrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLifecycleLeavesOnlyRootLock(t *testing.T) {
	root := testRoot(t)
	var warnings []string
	r := openRun(t, root, &warnings)
	if names := rootNames(t, root); !slices.Equal(names, []string{rootLockName}) {
		t.Fatalf("Open created %v; want only root.lock", names)
	}

	temp, err := r.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Dir(temp)
	if _, err := r.View(context.Background(), fileSource{key: "k", name: "f"}); err != nil {
		t.Fatal(err)
	}
	if err := startHelper(t, r, helperCommand("exit", codeVar+"=0")).Wait(); err != nil {
		t.Fatalf("Reviewer exit: %v", err)
	}
	want := []string{leaseName, ownerName, procsName, tempName, viewsName}
	if names := rootNames(t, runDir); !slices.Equal(names, want) {
		t.Fatalf("run dir holds %v; want %v", names, want)
	}
	if names := rootNames(t, filepath.Join(runDir, procsName)); len(names) != 0 {
		t.Fatalf("process record %v survived Wait", names)
	}

	r.Close()
	r.Close()
	if names := rootNames(t, root); !slices.Equal(names, []string{rootLockName}) {
		t.Fatalf("Close left %v; want only root.lock", names)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings %q", warnings)
	}
	if _, err := r.TempDir(); !errors.Is(err, errRunClosed) {
		t.Fatalf("TempDir after Close: %v; want %v", err, errRunClosed)
	}
}

func TestTempRedirectSeenByChildren(t *testing.T) {
	host := t.TempDir()
	root := testRoot(t)
	for _, name := range tempVariables {
		t.Setenv(name, host)
	}
	r := openRun(t, root, nil)
	temp, err := r.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range tempVariables {
		if got := os.Getenv(name); got != temp {
			t.Fatalf("%s=%q after TempDir; want %q", name, got, temp)
		}
	}

	inherited := helperCommand("env")
	explicit := helperCommand("env")
	explicit.Env = []string{helperVar + "=env", "TMPDIR=/elsewhere", "TEMP=/elsewhere"}
	for _, cmd := range []*exec.Cmd{inherited, explicit} {
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := startHelper(t, r, cmd).Wait(); err != nil {
			t.Fatal(err)
		}
		want := strings.Repeat(temp+"\n", len(tempVariables))
		if out.String() != want {
			t.Fatalf("child saw %q; want %q", out.String(), want)
		}
	}

	r.Close()
	for _, name := range tempVariables {
		if got := os.Getenv(name); got != host {
			t.Fatalf("%s=%q after Close; want %q", name, got, host)
		}
	}
}

func TestOpenRejectsRootThatIsAFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(Options{Root: root})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Open on a file: %v; want a not-a-directory error", err)
	}
}

func TestFromReturnsErrNoRunWithoutRun(t *testing.T) {
	if _, err := From(context.Background()); !errors.Is(err, ErrNoRun) {
		t.Fatalf("From(empty): %v; want ErrNoRun", err)
	}
	r := openRun(t, testRoot(t), nil)
	got, err := From(WithRun(context.Background(), r))
	if err != nil || got != r {
		t.Fatalf("From(WithRun) = %v, %v; want the run", got, err)
	}
}
