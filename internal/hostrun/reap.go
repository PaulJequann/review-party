package hostrun

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type claim struct {
	dir   string
	lease *lockFile
}

// Report is what one reap pass did.
type Report struct {
	// Removed lists the run directories this pass deleted completely.
	Removed []string
	// Leftovers lists what this pass could not delete.
	Leftovers []Leftover
	// Live counts the runs whose lease another process still holds.
	Live int
}

// Leftover is one path a reaper gave up on.
type Leftover struct {
	Path string
	Err  error
}

// Warnings renders one line per leftover in the wording the CLI prints.
func (r Report) Warnings() []string {
	lines := make([]string, 0, len(r.Leftovers))
	for _, left := range r.Leftovers {
		lines = append(lines, fmt.Sprintf("review-party could not remove %s: %v; run 'review-party clean' to remove it", left.Path, left.Err))
	}
	return lines
}

// Reap claims and removes every dead run under the root, waiting first for
// the removals Open started. Removed includes what Open's pass removed and
// no earlier Reap reported; Leftovers is only what is still there.
func (r *Run) Reap() Report {
	r.reaps.Wait()
	opened := r.takeOpenRemoved()
	claims, live, err := r.claimDead(r.currentID())
	if err != nil {
		return Report{Removed: opened, Leftovers: []Leftover{{Path: r.root, Err: err}}}
	}
	report := r.removeClaims(claims)
	report.Removed = append(opened, report.Removed...)
	report.Live = live
	return report
}

func (r *Run) takeOpenRemoved() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := r.openRemoved
	r.openRemoved = nil
	return removed
}

func (r *Run) claimDead(own string) ([]claim, int, error) {
	rootLock, err := lockRoot(r.root)
	if err != nil {
		return nil, 0, err
	}
	defer rootLock.close() //nolint:errcheck // Closing the root lock releases it; nothing else can be done on failure.
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, 0, err
	}
	var claims []claim
	live := 0
	for _, entry := range entries {
		if !isRunEntry(entry) || entry.Name() == own {
			continue
		}
		dir := filepath.Join(r.root, entry.Name())
		lease, dead, err := claimLease(dir)
		if err != nil {
			r.warn(fmt.Sprintf("review-party could not inspect %s: %v; run 'review-party clean' to remove it", dir, err))
			continue
		}
		if !dead {
			live++
			continue
		}
		claims = append(claims, claim{dir: dir, lease: lease})
	}
	return claims, live, nil
}

func isRunEntry(entry fs.DirEntry) bool {
	return entry.IsDir() && runIDPattern.MatchString(entry.Name())
}

func claimLease(dir string) (lease *lockFile, dead bool, err error) {
	lease, err = openLockFile(filepath.Join(dir, leaseName), false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	locked, err := lease.tryLock()
	if err != nil || !locked {
		closeLease(lease)
		return nil, false, err
	}
	return lease, true, nil
}

func (r *Run) removeClaims(claims []claim) Report {
	var report Report
	for _, c := range claims {
		if err := removeClaim(r.root, c); err != nil {
			report.Leftovers = append(report.Leftovers, Leftover{Path: c.dir, Err: err})
			continue
		}
		report.Removed = append(report.Removed, c.dir)
	}
	return report
}

const removeRetryDelay = 250 * time.Millisecond

func removeClaim(root string, c claim) error {
	killRecorded(readProcessRecords(c.dir))
	var err error
	for attempt := range removeAttempts {
		if attempt > 0 {
			time.Sleep(removeRetryDelay)
		}
		if err = removeAllButLease(c.dir); err == nil {
			break
		}
	}
	if err != nil {
		closeLease(c.lease)
		return err
	}
	rootLock, err := lockRoot(root)
	if err != nil {
		closeLease(c.lease)
		return err
	}
	defer rootLock.close() //nolint:errcheck // Closing the root lock releases it; nothing else can be done on failure.
	closeLease(c.lease)
	return removeShell(c.dir)
}

func removeAllButLease(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == leaseName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func removeShell(dir string) error {
	var err error
	for attempt := range removeAttempts {
		if attempt > 0 {
			time.Sleep(removeRetryDelay)
		}
		err = removeIfPresent(filepath.Join(dir, leaseName))
		if err == nil {
			err = removeIfPresent(dir)
		}
		if err == nil {
			return nil
		}
	}
	return err
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// A sentinel whose owner died is still sweeping descendants that left the
// Reviewer's group, so it gets longer than its own sweep budget to finish
// before it is killed.
const sentinelSweepWait = 3 * time.Second

func killRecorded(records []ProcessRecord) {
	boot, err := bootID()
	if err != nil {
		return
	}
	for _, record := range records {
		if !sameBoot(record.Reviewer.Boot, boot) {
			continue
		}
		if record.Group != 0 {
			killGroup(record.Group, record.Reviewer)
		}
		killProcess(record.Reviewer)
		record.Sentinel.awaitExit(sentinelSweepWait)
		killProcess(record.Sentinel)
	}
}

// State is what a reaper would do with a run.
type State string

const (
	// StateLive: another process holds the lease.
	StateLive State = "live"
	// StateDead: nobody holds the lease, so the next Open removes it.
	StateDead State = "dead"
	// StateUnknown: the entry is foreign or its lease could not be inspected.
	StateUnknown State = "unknown"
)

// Entry is one thing under the runtime root, as the footprint command shows
// it. Foreign entries are files or directories review-party did not create.
type Entry struct {
	ID        string
	Path      string
	State     State
	Foreign   bool
	Owner     *OwnerRecord
	Reviewers []ProcessRecord
	Views     int
	Bytes     int64
	Err       error
}

// Inventory lists every entry under root without claiming or removing
// anything.
func Inventory(root string) ([]Entry, error) {
	entries, err := snapshotEntries(root)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Bytes, err = DiskUsage(entries[i].Path)
		if err != nil && entries[i].Err == nil {
			entries[i].Err = err
		}
	}
	return entries, nil
}

func snapshotEntries(root string) ([]Entry, error) {
	rootLock, err := lockRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootLock.close() //nolint:errcheck // Closing the root lock releases it; nothing else can be done on failure.
	names, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, name := range names {
		if name.Name() == rootLockName {
			continue
		}
		entries = append(entries, inspectEntry(root, name))
	}
	return entries, nil
}

func inspectEntry(root string, name fs.DirEntry) Entry {
	entry := Entry{ID: name.Name(), Path: filepath.Join(root, name.Name()), State: StateUnknown}
	if !isRunEntry(name) {
		entry.Foreign = true
		return entry
	}
	entry.State, entry.Err = probeLease(entry.Path)
	var owner OwnerRecord
	if err := readRecord(filepath.Join(entry.Path, ownerName), &owner); err == nil {
		entry.Owner = &owner
	}
	entry.Reviewers = readProcessRecords(entry.Path)
	if views, err := os.ReadDir(filepath.Join(entry.Path, viewsName)); err == nil {
		entry.Views = len(views)
	}
	return entry
}

func probeLease(dir string) (State, error) {
	lease, dead, err := claimLease(dir)
	if err != nil {
		return StateUnknown, err
	}
	closeLease(lease)
	if dead {
		return StateDead, nil
	}
	return StateLive, nil
}

// DiskUsage sums the sizes of the regular files at or under path without
// following symlinks.
func DiskUsage(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
