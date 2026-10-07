package hostrun

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLiveRunIsNeverClaimed(t *testing.T) {
	root := testRoot(t)
	live := openRun(t, root, nil)
	temp, err := live.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	liveDir := filepath.Dir(temp)

	other := openRun(t, root, nil)
	rep := other.Reap()
	if !reflect.DeepEqual(rep, Report{Live: 1}) {
		t.Fatalf("in-process reap of a live run reported %+v", rep)
	}
	if out, err := runReaper(root); err != nil || out != "" {
		t.Fatalf("reaper process: %v, warned %q", err, out)
	}
	if !fileExists(filepath.Join(liveDir, leaseName)) {
		t.Fatalf("live run %s lost its lease", liveDir)
	}
}

func TestReapReportsTheRunsOpenRemovedOnce(t *testing.T) {
	root := testRoot(t)
	dead := fakeDeadRun(t, root, "1-dead")
	r := openRun(t, root, nil)
	if rep := r.Reap(); !reflect.DeepEqual(rep, Report{Removed: []string{dead}}) {
		t.Fatalf("first Reap reported %+v; want the run Open removed", rep)
	}
	if rep := r.Reap(); !reflect.DeepEqual(rep, Report{}) {
		t.Fatalf("second Reap reported %+v; want nothing", rep)
	}
	if fileExists(dead) {
		t.Fatalf("dead run %s survived", dead)
	}
}

func runReaper(root string) (string, error) {
	out, err := helperCommand("reaper", rootVar+"="+root).CombinedOutput()
	return string(out), err
}

func TestConcurrentReapersRemoveDeadRunsWithoutWarnings(t *testing.T) {
	root := testRoot(t)
	for _, id := range []string{"1-aaaa", "2-bbbb", "3-cccc"} {
		fakeDeadRun(t, root, id)
	}
	const reapers = 4
	outputs := make([]string, reapers)
	errs := make([]error, reapers)
	var wg sync.WaitGroup
	for i := range reapers {
		wg.Go(func() { outputs[i], errs[i] = runReaper(root) })
	}
	var warnings []string
	openRun(t, root, &warnings).Reap()
	wg.Wait()
	for i := range reapers {
		if errs[i] != nil || outputs[i] != "" {
			t.Fatalf("reaper %d: %v, warned %q", i, errs[i], outputs[i])
		}
	}
	if len(warnings) != 0 {
		t.Fatalf("in-process reaper warned %q", warnings)
	}
	if names := rootNames(t, root); !slices.Equal(names, []string{rootLockName}) {
		t.Fatalf("root holds %v; want only root.lock", names)
	}
}

func TestReapKillsOnlyRecordsWhoseStartStampMatches(t *testing.T) {
	root := testRoot(t)
	bystander := startChild(t, helperCommand("sleep"))
	victim := startChild(t, helperCommand("sleep"))
	bystanderID, err := identify(bystander.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	victimID, err := identify(victim.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	stale := bystanderID
	stale.Start++
	fakeDeadRun(t, root, "1-dead",
		ProcessRecord{Schema: recordSchema, Sentinel: stale, Reviewer: stale, Started: fakeTime, Program: "stale", Mode: TreeGroup},
		ProcessRecord{Schema: recordSchema, Sentinel: stale, Reviewer: victimID, Started: fakeTime, Program: "victim", Mode: TreeGroup})

	rep := openRun(t, root, nil).Reap()
	if len(rep.Leftovers) != 0 {
		t.Fatalf("reap left %+v", rep.Leftovers)
	}
	waitFor(t, 2*time.Second, "the recorded Reviewer to die", func() bool { return !running(victim.Process.Pid) })
	if !running(bystander.Process.Pid) {
		t.Fatal("a record with the wrong start stamp was signalled")
	}
}

func TestInventoryReportsEveryEntryExactly(t *testing.T) {
	root := testRoot(t)
	live := openRun(t, root, nil)
	temp, err := live.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	liveDir := filepath.Dir(temp)
	record := ProcessRecord{Schema: recordSchema, Sentinel: Identity{PID: 1, Start: 2, Boot: "b"}, Reviewer: Identity{PID: 3, Start: 4, Boot: "b"}, Group: 3, Started: fakeTime, Program: "fake", Mode: TreeGroup}
	deadDir := fakeDeadRun(t, root, "1-dead", record)
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "junk"), 0o700); err != nil {
		t.Fatal(err)
	}

	var liveOwner OwnerRecord
	readJSON(t, filepath.Join(liveDir, ownerName), &liveOwner)
	want := []Entry{
		{ID: "1-dead", Path: deadDir, State: StateDead, Owner: &OwnerRecord{Schema: recordSchema, ID: "1-dead", Created: fakeTime, Command: "fake"}, Reviewers: []ProcessRecord{record}, Views: 1, Bytes: fileSize(t, filepath.Join(deadDir, ownerName)) + fileSize(t, filepath.Join(deadDir, procsName, "1000.json")) + 3},
		{ID: filepath.Base(liveDir), Path: liveDir, State: StateLive, Owner: &liveOwner, Bytes: fileSize(t, filepath.Join(liveDir, ownerName))},
		{ID: "junk", Path: filepath.Join(root, "junk"), State: StateUnknown, Foreign: true},
		{ID: "stray.txt", Path: filepath.Join(root, "stray.txt"), State: StateUnknown, Foreign: true, Bytes: 5},
	}
	got, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(want, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Inventory =\n%s\nwant\n%s", describeEntries(t, got), describeEntries(t, want))
	}
	if !fileExists(deadDir) {
		t.Fatal("Inventory removed the dead run")
	}
}

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func describeEntries(t *testing.T, entries []Entry) string {
	t.Helper()
	var text bytes.Buffer
	encoder := json.NewEncoder(&text)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		t.Fatal(err)
	}
	return text.String()
}
