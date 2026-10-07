//go:build windows

package hostrun

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestForeignOpenHandleBecomesAWarningUntilItCloses(t *testing.T) {
	root := testRoot(t)
	dir := fakeDeadRun(t, root, "1-dead")
	ready := filepath.Join(t.TempDir(), "ready")
	holder := startChild(t, helperCommand("hold", holdVar+"="+filepath.Join(dir, tempName, "scratch.txt"), readyVar+"="+ready))
	waitFor(t, 5*time.Second, "the holder to open the file", func() bool { return fileExists(ready) })

	var warnings []string
	rep := openRun(t, root, &warnings).Reap()
	if !fileExists(dir) {
		t.Fatal("the run vanished while a foreign handle held a file in it")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], dir) || !strings.Contains(warnings[0], "review-party clean") {
		t.Fatalf("Open warned %q; want one line naming %s and review-party clean", warnings, dir)
	}
	if len(rep.Leftovers) != 1 || rep.Leftovers[0].Path != dir {
		t.Fatalf("Reap reported %+v; want one leftover for %s", rep, dir)
	}

	_ = holder.Process.Kill() //nolint:errcheck // The holder only has to stop holding the handle.
	_ = holder.Wait()         //nolint:errcheck // Same.
	openRun(t, root, nil).Reap()
	if names := rootNames(t, root); !slices.Equal(names, []string{rootLockName}) {
		t.Fatalf("root holds %v after the handle closed; want only root.lock", names)
	}
}
