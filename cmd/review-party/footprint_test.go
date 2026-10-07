package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/hostrun"
)

// hostFixture is one isolated host: its own HOME, XDG directories, host
// temp directory, runtime root, and cache.
type hostFixture struct {
	t     *testing.T
	host  hostDirectories
	state string
}

func newHostFixture(t *testing.T) hostFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	temp := filepath.Join(home, "tmp")
	if err := os.Mkdir(temp, 0o700); err != nil {
		t.Fatal(err)
	}
	host := hostDirectories{temp: temp, runtimeRoot: filepath.Join(temp, "review-party-runtime-test"), cache: filepath.Join(home, "cache", "review-party", "model-discovery")}
	return hostFixture{t: t, host: host, state: filepath.Join(home, "state", "review-party")}
}

// seed gives the host one of everything a footprint lists and returns the
// dead run's directory.
func (fixture hostFixture) seed() string {
	fixture.t.Helper()
	dead := filepath.Join(fixture.host.runtimeRoot, "1-dead")
	fixture.write(filepath.Join(dead, "lease"), "")
	fixture.write(filepath.Join(dead, "owner.json"), `{"schema":1,"id":"1-dead","owner":{"pid":1,"start":1,"boot":"gone"},"command":"run","version":"v0"}`)
	fixture.write(filepath.Join(dead, "t", "scratch"), "reviewer temp")
	fixture.write(filepath.Join(fixture.state, "ledger.sqlite"), "ledger")
	fixture.write(filepath.Join(fixture.state, "ledger.sqlite-wal"), "wal")
	fixture.write(filepath.Join(fixture.state, "backups", "ledger-1.sqlite"), "old ledger")
	fixture.write(filepath.Join(fixture.state, "artifacts", "ab", "published"), "published")
	fixture.write(filepath.Join(fixture.state, "artifacts", ".artifact-1.tmp"), "partial")
	fixture.write(filepath.Join(fixture.state, "notes.txt"), "the user's")
	fixture.write(filepath.Join(fixture.host.cache, "codex.json"), "{}")
	fixture.write(filepath.Join(fixture.host.temp, "review-party-worktrees", "abc", "file"), "old worktree")
	fixture.write(filepath.Join(fixture.host.temp, "review-party-delta-1", "patch"), "old delta")
	fixture.age(fixture.inTemp("review-party-worktrees", "review-party-delta-1")...)
	fixture.write(filepath.Join(fixture.host.temp, "review-party-runtime-9999", "x"), "another root")
	fixture.write(filepath.Join(fixture.host.temp, "unrelated", "x"), "someone else's")
	return dead
}

func (fixture hostFixture) write(path, content string) {
	fixture.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fixture.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		fixture.t.Fatal(err)
	}
}

// age backdates paths past legacyQuietPeriod, as leftovers of an earlier
// release would be.
func (fixture hostFixture) age(paths ...string) {
	fixture.t.Helper()
	old := time.Now().Add(-2 * legacyQuietPeriod)
	for _, path := range paths {
		if err := os.Chtimes(path, old, old); err != nil {
			fixture.t.Fatal(err)
		}
	}
}

// liveRun opens a run under the fixture's root and gives it a directory,
// as a concurrent review would.
func (fixture hostFixture) liveRun() string {
	fixture.t.Helper()
	run, err := hostrun.Open(hostrun.Options{Root: fixture.host.runtimeRoot, Command: "run"})
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.t.Cleanup(run.Close)
	temp, err := run.TempDir()
	if err != nil {
		fixture.t.Fatal(err)
	}
	return filepath.Dir(temp)
}

func (fixture hostFixture) run(arguments ...string) commandRun {
	fixture.t.Helper()
	var stdout, stderr bytes.Buffer
	streams := productionCommandIO(strings.NewReader(""), &stdout, &stderr)
	streams.host = fixture.host
	exit := execute(context.Background(), arguments, streams)
	return commandRun{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

// tree maps every path under the fixture's HOME to its contents, with
// directories as "/".
func (fixture hostFixture) tree() map[string]string {
	fixture.t.Helper()
	home := filepath.Dir(fixture.host.temp)
	tree := map[string]string{}
	err := filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[filepath.ToSlash(relative)] = "/"
			return nil
		}
		content, err := os.ReadFile(path)
		tree[filepath.ToSlash(relative)] = string(content)
		return err
	})
	if err != nil {
		fixture.t.Fatal(err)
	}
	return tree
}

func (fixture hostFixture) exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// assertOnDisk fails unless every present path exists and no absent one
// does.
func (fixture hostFixture) assertOnDisk(present, absent []string) {
	fixture.t.Helper()
	want := map[string]bool{}
	for _, path := range absent {
		want[path] = false
	}
	for _, path := range present {
		want[path] = true
	}
	for path, exists := range want {
		if fixture.exists(path) != exists {
			fixture.t.Fatalf("%s exists = %v, want %v", path, !exists, exists)
		}
	}
}

// assertCleanMatch fails unless the command exited 0 with nothing on stderr
// and its stdout matches pattern.
func assertCleanMatch(t *testing.T, name string, run commandRun, pattern *regexp.Regexp) {
	t.Helper()
	if run.exit != 0 || run.stderr != "" {
		t.Fatalf("%s = %+v, want exit 0 and no stderr", name, run)
	}
	if !pattern.MatchString(run.stdout) {
		t.Fatalf("%s stdout =\n%s\nwant a match for %s", name, run.stdout, pattern)
	}
}

func (fixture hostFixture) inState(names ...string) []string {
	return joinAll(fixture.state, names)
}

func (fixture hostFixture) inTemp(names ...string) []string {
	return joinAll(fixture.host.temp, names)
}

func joinAll(directory string, names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(directory, filepath.FromSlash(name)))
	}
	return paths
}

// labels renders items as "kind name" with the name relative to its
// location, sorted, so assertions name what was found.
func labels(items []footprintItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item.Kind)+" "+filepath.Base(item.Path))
	}
	slices.Sort(out)
	return out
}

func decodeInto[T any](t *testing.T, result commandRun, exit int) T {
	t.Helper()
	var value T
	if result.exit != exit {
		t.Fatalf("run = %+v, want exit %d", result, exit)
	}
	if err := json.Unmarshal([]byte(result.stdout), &value); err != nil {
		t.Fatalf("decode %q: %v", result.stdout, err)
	}
	return value
}

func TestFootprintListsEverythingReviewPartyOwnsAndChangesNothing(t *testing.T) {
	fixture := newHostFixture(t)
	live := fixture.liveRun()
	fixture.seed()
	before := fixture.tree()

	report := decodeInto[footprint](t, fixture.run("footprint", "--format", "json"), 0)

	want := []string{
		"artifacts artifacts", "backups backups", "cache model-discovery",
		"ledger ledger.sqlite", "ledger ledger.sqlite-wal", "legacy review-party-delta-1", "legacy review-party-worktrees",
		"live-run " + filepath.Base(live), "orphaned-run 1-dead", "partial-artifact .artifact-1.tmp", "unrecognized notes.txt",
	}
	if got := labels(report.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("footprint items =\n%v\nwant\n%v", got, want)
	}
	reasons := map[footprintKind]string{}
	for _, item := range report.Items {
		reasons[item.Kind] = item.Reason
	}
	guarded := "a Review Party run is live"
	wantReasons := map[footprintKind]string{
		kindArtifacts: guarded, kindBackups: guarded, kindLedger: guarded, kindPartial: guarded, kindCache: "", kindLegacy: "", kindOrphanedRun: "",
		kindLiveRun: "its owner is still running", kindUnrecognized: "Review Party did not write it",
	}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("reasons by kind =\n%v\nwant\n%v", reasons, wantReasons)
	}
	if after := fixture.tree(); !reflect.DeepEqual(before, after) {
		t.Fatalf("footprint changed the host:\nbefore %v\nafter  %v", before, after)
	}
}

func TestFootprintOfAnUntouchedHostIsEmptyAndCreatesNothing(t *testing.T) {
	fixture := newHostFixture(t)
	before := fixture.tree()

	result := fixture.run("footprint")

	assertRun(t, result, commandRun{stdout: "Footprint: 0 items, 0 B; 0 removable now\n"})
	if after := fixture.tree(); !reflect.DeepEqual(before, after) {
		t.Fatalf("footprint changed an untouched host:\nbefore %v\nafter  %v", before, after)
	}
}

func TestFootprintHumanLineNamesTheRunAndItsRemoval(t *testing.T) {
	fixture := newHostFixture(t)
	dead := fixture.seed()

	result := fixture.run("footprint")

	pattern := regexp.MustCompile(`(?m)^orphaned-run ` + regexp.QuoteMeta(dead) + ` \d+ B; run dead of run by pid 1 version v0, 0 Reviewers, 0 views; remove with: review-party clean\n` +
		`(?s:.*)Footprint: 10 items, \d+ B; 9 removable now; 4 left over \(\d+ B\); fix: review-party clean --yes\n\z`)
	assertCleanMatch(t, "footprint", result, pattern)
}
