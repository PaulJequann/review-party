package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"reviewparty/internal/discovery"
	"reviewparty/internal/engine"
	"reviewparty/internal/hostrun"
)

// hostDirectories are the host locations Review Party writes outside the
// state directory. Empty fields mean the production defaults; tests set them
// because hostrun captures the host temp directory at process start.
type hostDirectories struct {
	temp        string
	runtimeRoot string
	cache       string
}

func (host hostDirectories) withDefaults() hostDirectories {
	if host.temp == "" {
		host.temp = hostrun.HostTempDir()
	}
	if host.runtimeRoot == "" {
		host.runtimeRoot = hostrun.DefaultRoot()
	}
	return host
}

func (host hostDirectories) cacheDirectory() (string, error) {
	if host.cache != "" {
		return host.cache, nil
	}
	return discovery.CacheDirectory()
}

type footprintClass string

const (
	classRuntime footprintClass = "runtime"
	classDurable footprintClass = "durable"
	classCache   footprintClass = "cache"
	classLegacy  footprintClass = "legacy"
)

type footprintKind string

const (
	kindOrphanedRun   footprintKind = "orphaned-run"
	kindLiveRun       footprintKind = "live-run"
	kindUnreadableRun footprintKind = "unreadable-run"
	kindForeign       footprintKind = "foreign"
	kindLedger                      = footprintKind(engine.StateLedger)
	kindBackups                     = footprintKind(engine.StateBackups)
	kindArtifacts                   = footprintKind(engine.StateArtifacts)
	kindPartial                     = footprintKind(engine.StatePartialArtifact)
	kindUnrecognized                = footprintKind(engine.StateUnrecognized)
	kindCache         footprintKind = "cache"
	kindLegacy        footprintKind = "legacy"
	kindRecentTemp    footprintKind = "recent-temp"
)

// legacyQuietPeriod is how long a review-party-* entry in the host temp
// directory must sit unchanged before clean treats it as left over. The
// current release still names some host temp files this way, such as the
// instruction editor's open draft, and an older binary may still be running.
const legacyQuietPeriod = 24 * time.Hour

// removal is what asks clean to remove an item.
type removal int

const (
	removedNever removal = iota
	// removedByReap: plain clean, through hostrun's reaper.
	removedByReap
	removedWithYes
	// removedWhenSelected: --yes plus the kind's own selector flag, so a
	// habitual --yes never deletes review history.
	removedWhenSelected
)

type footprintRule struct {
	class   footprintClass
	removal removal
	flag    string
	// blocked is why an item of this kind is never removable now.
	blocked string
	// guarded kinds stay while any run is live, because its process may be
	// writing them.
	guarded bool
	// leftover kinds are state no process needs, which doctor reports.
	leftover bool
}

var footprintRules = map[footprintKind]footprintRule{
	kindOrphanedRun:   {class: classRuntime, removal: removedByReap, leftover: true},
	kindLiveRun:       {class: classRuntime, blocked: "its owner is still running"},
	kindUnreadableRun: {class: classRuntime, removal: removedByReap, blocked: "its lease could not be inspected"},
	kindForeign:       {class: classRuntime, blocked: "Review Party did not create it"},
	kindLedger:        {class: classDurable, removal: removedWhenSelected, flag: "ledger", guarded: true},
	kindBackups:       {class: classDurable, removal: removedWhenSelected, flag: "backups", guarded: true},
	kindArtifacts:     {class: classDurable, removal: removedWhenSelected, flag: "artifacts", guarded: true},
	kindPartial:       {class: classDurable, removal: removedWithYes, guarded: true, leftover: true},
	kindUnrecognized:  {class: classDurable, blocked: "Review Party did not write it"},
	kindCache:         {class: classCache, removal: removedWithYes},
	kindLegacy:        {class: classLegacy, removal: removedWithYes, leftover: true},
	kindRecentTemp:    {class: classLegacy, blocked: "changed in the last 24 hours, so a running Review Party may still use it"},
}

// command is the clean invocation that removes an item of this kind.
func (rule footprintRule) command(configurationPath string) string {
	var flags string
	switch rule.removal {
	case removedNever:
		return ""
	case removedByReap:
	case removedWithYes:
		flags = " --yes"
	case removedWhenSelected:
		flags = " --yes --" + rule.flag
	}
	return "review-party clean" + flags + configurationArgument(configurationPath)
}

type footprintItem struct {
	Path      string         `json:"path"`
	Kind      footprintKind  `json:"kind"`
	Class     footprintClass `json:"class"`
	Bytes     int64          `json:"bytes"`
	Removable bool           `json:"removable"`
	// Reason is why the item is not removable now.
	Reason string        `json:"reason,omitempty"`
	Remove string        `json:"remove,omitempty"`
	Error  string        `json:"error,omitempty"`
	Run    *footprintRun `json:"run,omitempty"`
}

type footprintRun struct {
	State     hostrun.State       `json:"state"`
	Command   string              `json:"command,omitempty"`
	Version   string              `json:"version,omitempty"`
	Created   time.Time           `json:"created,omitzero"`
	PID       int                 `json:"pid,omitempty"`
	Reviewers []footprintReviewer `json:"reviewers"`
	Views     int                 `json:"views"`
}

type footprintReviewer struct {
	PID     int    `json:"pid"`
	Program string `json:"program"`
}

type footprintLocations struct {
	Runtime string `json:"runtime"`
	State   string `json:"state,omitempty"`
	Cache   string `json:"cache,omitempty"`
	Temp    string `json:"temp"`
}

type footprintProblem struct {
	Path  string `json:"path,omitempty"`
	Error string `json:"error"`
}

type footprint struct {
	Locations  footprintLocations `json:"locations"`
	Items      []footprintItem    `json:"items"`
	Unreadable []footprintProblem `json:"unreadable"`
	// configuration is the --config path the remove commands repeat.
	configuration string
}

// takeFootprint lists everything Review Party owns on this host without
// creating, claiming, or removing anything. A missing location has no items.
func takeFootprint(host hostDirectories, configurationPath string) footprint {
	host = host.withDefaults()
	result := footprint{
		Locations: footprintLocations{Runtime: host.runtimeRoot, Temp: host.temp}, Items: []footprintItem{}, Unreadable: []footprintProblem{},
		configuration: configurationPath,
	}
	result.collect(host.runtimeRoot, runtimeItems)
	if state, err := engine.StateDirectory(configurationPath); err != nil {
		result.unreadable("", err)
	} else {
		result.Locations.State = state
		result.collect(state, stateItems)
	}
	if cache, err := host.cacheDirectory(); err != nil {
		result.unreadable("", err)
	} else {
		result.Locations.Cache = cache
		result.collect(cache, cacheItems)
	}
	owned := ownedDirectories(result.Locations.State, result.Locations.Cache)
	result.collect(host.temp, func(temp string) ([]footprintItem, error) { return legacyItems(temp, owned) })
	result.judge()
	return result
}

func (result *footprint) collect(location string, source func(string) ([]footprintItem, error)) {
	items, err := source(location)
	if err != nil {
		result.unreadable(location, err)
	}
	result.Items = append(result.Items, items...)
}

func (result *footprint) unreadable(path string, err error) {
	result.Unreadable = append(result.Unreadable, footprintProblem{Path: path, Error: err.Error()})
}

func (result *footprint) judge() {
	live := false
	for _, item := range result.Items {
		live = live || item.Kind == kindLiveRun
	}
	for i := range result.Items {
		item := &result.Items[i]
		rule := footprintRules[item.Kind]
		item.Class = rule.class
		item.Remove = rule.command(result.configuration)
		switch {
		case rule.blocked != "":
			item.Reason = rule.blocked
		case rule.guarded && live:
			item.Reason = "a Review Party run is live"
		}
		item.Removable = item.Reason == ""
	}
}

func newFootprintItem(path string, kind footprintKind, bytes int64, err error) footprintItem {
	item := footprintItem{Path: path, Kind: kind, Bytes: bytes}
	if err != nil {
		item.Error = err.Error()
	}
	return item
}

// existingDirectory reports whether path is a real directory, so a missing
// location is empty rather than an error, and a symlink is never followed.
func existingDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, &fs.PathError{Op: "inventory", Path: path, Err: errors.New("not a directory")}
	}
	return true, nil
}

func runtimeItems(root string) ([]footprintItem, error) {
	if exists, err := existingDirectory(root); !exists {
		return nil, err
	}
	entries, err := hostrun.Inventory(root)
	if err != nil {
		return nil, err
	}
	items := make([]footprintItem, 0, len(entries))
	for _, entry := range entries {
		item := newFootprintItem(entry.Path, runtimeKind(entry), entry.Bytes, entry.Err)
		if !entry.Foreign {
			item.Run = newFootprintRun(entry)
		}
		items = append(items, item)
	}
	return items, nil
}

func runtimeKind(entry hostrun.Entry) footprintKind {
	switch {
	case entry.Foreign:
		return kindForeign
	case entry.State == hostrun.StateLive:
		return kindLiveRun
	case entry.State == hostrun.StateDead:
		return kindOrphanedRun
	default:
		return kindUnreadableRun
	}
}

func newFootprintRun(entry hostrun.Entry) *footprintRun {
	run := &footprintRun{State: entry.State, Views: entry.Views, Reviewers: []footprintReviewer{}}
	if entry.Owner != nil {
		run.Command, run.Version, run.Created, run.PID = entry.Owner.Command, entry.Owner.Version, entry.Owner.Created, entry.Owner.Owner.PID
	}
	for _, record := range entry.Reviewers {
		run.Reviewers = append(run.Reviewers, footprintReviewer{PID: record.Reviewer.PID, Program: record.Program})
	}
	return run
}

func stateItems(directory string) ([]footprintItem, error) {
	entries, err := engine.InventoryState(directory)
	items := make([]footprintItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, newFootprintItem(entry.Path, footprintKind(entry.Kind), entry.Bytes, entry.Err))
	}
	return items, err
}

func cacheItems(directory string) ([]footprintItem, error) {
	if exists, err := existingDirectory(directory); !exists {
		return nil, err
	}
	bytes, err := hostrun.DiskUsage(directory)
	return []footprintItem{newFootprintItem(directory, kindCache, bytes, err)}, nil
}

// ownedDirectories are the owned locations and every existing directory
// above them. They are matched by identity, not spelling, because a
// symlinked host temp directory spells the same directory differently.
func ownedDirectories(locations ...string) []fs.FileInfo {
	var owned []fs.FileInfo
	for _, location := range locations {
		for path := location; path != ""; path = filepath.Dir(path) {
			if info, err := os.Stat(path); err == nil {
				owned = append(owned, info)
			}
			if filepath.Dir(path) == path {
				break
			}
		}
	}
	return owned
}

// legacyItems are what releases before the runtime root left directly in
// the host temp directory, such as review-party-worktrees. An entry that is
// or holds one of the owned locations, such as a state directory configured
// under the host temp directory, is never legacy.
func legacyItems(temp string, owned []fs.FileInfo) ([]footprintItem, error) {
	entries, err := os.ReadDir(temp)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var items []footprintItem
	for _, entry := range entries {
		path := filepath.Join(temp, entry.Name())
		if !isLegacyEntry(entry) || holdsOwned(path, owned) {
			continue
		}
		bytes, changed, sizeErr := legacyUsage(path)
		items = append(items, newFootprintItem(path, legacyKind(changed, sizeErr), bytes, sizeErr))
	}
	return items, err
}

func holdsOwned(path string, owned []fs.FileInfo) bool {
	info, err := os.Lstat(path)
	return err == nil && slices.ContainsFunc(owned, func(directory fs.FileInfo) bool { return os.SameFile(info, directory) })
}

// legacyUsage sizes an entry and finds its newest change anywhere inside,
// because writing deep in a tree leaves the top entry's time alone.
func legacyUsage(path string) (bytes int64, changed time.Time, err error) {
	err = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		if info.ModTime().After(changed) {
			changed = info.ModTime()
		}
		return nil
	})
	return bytes, changed, err
}

// legacyKind keeps an entry it could not fully read, since an unread part
// may be in use.
func legacyKind(changed time.Time, err error) footprintKind {
	if err != nil || time.Since(changed) < legacyQuietPeriod {
		return kindRecentTemp
	}
	return kindLegacy
}

// isLegacyEntry matches review-party and review-party-*, except runtime
// roots, which are never legacy, and entries another user owns.
func isLegacyEntry(entry fs.DirEntry) bool {
	name := entry.Name()
	if name != "review-party" && !strings.HasPrefix(name, "review-party-") {
		return false
	}
	return !strings.HasPrefix(name, "review-party-runtime") && ownedByCurrentUser(entry)
}
