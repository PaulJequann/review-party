// Package hostrun owns everything Review Party leaves on the host while one
// process runs: a run directory inside one runtime root, the lease that
// proves the owner is alive, the execution views Reviewers read, the
// temporary directory every child inherits, and the sentinel-supervised
// processes Reviewers run in.
//
// A run that ends normally removes all of it. A run that dies is reaped by
// the next Open in any process. Nothing here fails a command because of
// cleanup: every cleanup and reap failure becomes one warning naming
// `review-party clean`.
package hostrun

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Options configure Open.
type Options struct {
	// Root overrides the runtime root. Empty means the per-user root under
	// the host temp directory.
	Root string
	// Warn receives one line per cleanup failure. Nil discards them.
	Warn func(string)
	// Command is the invocation recorded in owner.json, such as "run".
	Command string
	// Version is the review-party build recorded in owner.json.
	Version string
}

// Run is one process's run. Get one from Open. All methods are safe for
// concurrent use.
type Run struct {
	root    string
	warn    func(string)
	command string
	version string

	mu        sync.Mutex
	id        string
	dir       string
	lease     *lockFile
	closed    bool
	degraded  bool
	procs     map[*Process]struct{}
	views     map[string]*viewEntry
	viewCount int
	hostEnv   map[string]string
	// openRemoved is what Open's background pass removed, until Reap
	// reports it.
	openRemoved []string

	reaps        sync.WaitGroup
	builds       sync.WaitGroup
	buildCtx     context.Context
	cancelBuilds context.CancelFunc
}

var hostTemp = os.TempDir()

// HostTempDir is the temporary directory the host gave this process, never
// the run's redirected one.
func HostTempDir() string { return hostTemp }

// Unix reads TMPDIR; Windows reads TMP then TEMP.
var tempVariables = []string{"TMPDIR", "TMP", "TEMP"}

var errRunClosed = errors.New("the run is closed")

// Open validates the root, claims every dead run under it, and starts their
// removal in the background. It creates no run directory: the first TempDir,
// View, or Start does. Open returns an error only when this process cannot
// use the root at all; the caller warns and continues, and Reviewer paths
// then fail with ErrNoRun.
func Open(options Options) (*Run, error) {
	root := options.Root
	if root == "" {
		root = DefaultRoot()
	}
	if err := validateRoot(root); err != nil {
		return nil, err
	}
	warn := options.Warn
	if warn == nil {
		warn = func(string) {}
	}
	buildCtx, cancelBuilds := context.WithCancel(context.Background())
	r := &Run{
		root:         root,
		warn:         warn,
		command:      options.Command,
		version:      options.Version,
		procs:        map[*Process]struct{}{},
		views:        map[string]*viewEntry{},
		buildCtx:     buildCtx,
		cancelBuilds: cancelBuilds,
	}
	claims, _, err := r.claimDead("")
	if err != nil {
		cancelBuilds()
		return nil, err
	}
	r.reaps.Add(1)
	go func() {
		defer r.reaps.Done()
		report := r.removeClaims(claims)
		r.mu.Lock()
		r.openRemoved = report.Removed
		r.mu.Unlock()
		r.warnReport(report)
	}()
	return r, nil
}

// TempDir is <run>/t, creating the run on first use.
func (r *Run) TempDir() (string, error) {
	dir, err := r.ensureDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tempName), nil
}

func (r *Run) ensureDir() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", errRunClosed
	}
	if r.dir != "" {
		return r.dir, nil
	}
	rootLock, err := lockRoot(r.root)
	if err != nil {
		return "", err
	}
	defer rootLock.close() //nolint:errcheck // Closing the root lock releases it; nothing else can be done on failure.
	id, dir, lease, err := r.createRunDir()
	if err != nil {
		return "", err
	}
	r.id, r.dir, r.lease = id, dir, lease
	r.redirectTemp(filepath.Join(dir, tempName))
	return dir, nil
}

func (r *Run) createRunDir() (id, dir string, lease *lockFile, err error) {
	id, dir, err = r.mkdirRunID()
	if err != nil {
		return "", "", nil, err
	}
	self, _ := identify(os.Getpid()) //nolint:errcheck // The owner record is advisory; an unknown identity stays zero.
	executable, _ := os.Executable() //nolint:errcheck // Same: the record names the executable when the platform can.
	record := OwnerRecord{
		Schema:     recordSchema,
		ID:         id,
		Owner:      self,
		Created:    time.Now().UTC(),
		Command:    r.command,
		Version:    r.version,
		Executable: executable,
	}
	lease, err = openLockFile(filepath.Join(dir, leaseName), true)
	if err == nil {
		err = populateRunDir(dir, lease, record)
	}
	if err != nil {
		closeLease(lease)
		_ = os.RemoveAll(dir) //nolint:errcheck // A half-made run has no lease holder, so the next Open removes it.
		return "", "", nil, err
	}
	return id, dir, lease, nil
}

// Ids from the same second share 65536 suffixes, so a live run can already
// hold the one drawn.
const runIDAttempts = 8

func (r *Run) mkdirRunID() (id, dir string, err error) {
	for range runIDAttempts {
		if id, err = newRunID(); err != nil {
			return "", "", err
		}
		dir = filepath.Join(r.root, id)
		if err = os.Mkdir(dir, 0o700); !errors.Is(err, fs.ErrExist) {
			return id, dir, err
		}
	}
	return "", "", err
}

func populateRunDir(dir string, lease *lockFile, record OwnerRecord) error {
	locked, err := lease.tryLock()
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("the lease of new run %s is already locked", record.ID)
	}
	if err := writeRecord(filepath.Join(dir, ownerName), record); err != nil {
		return err
	}
	return os.Mkdir(filepath.Join(dir, tempName), 0o700)
}

func (r *Run) redirectTemp(dir string) {
	r.hostEnv = map[string]string{}
	for _, name := range tempVariables {
		if value, ok := os.LookupEnv(name); ok {
			r.hostEnv[name] = value
		}
		_ = os.Setenv(name, dir) //nolint:errcheck // The names are fixed; a refused value leaves the host setting, which children inherit.
	}
}

func (r *Run) restoreTemp() {
	for _, name := range tempVariables {
		if value, ok := r.hostEnv[name]; ok {
			_ = os.Setenv(name, value) //nolint:errcheck // Restoring the host value is best effort at Close.
		} else {
			_ = os.Unsetenv(name) //nolint:errcheck // Same.
		}
	}
}

// Close stops any Reviewer still running, joins background work, then
// removes the run directory. It never returns an error and is idempotent.
func (r *Run) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	procs := make([]*Process, 0, len(r.procs))
	for p := range r.procs {
		procs = append(procs, p)
	}
	dir, lease, degraded := r.dir, r.lease, r.degraded
	r.mu.Unlock()

	for _, p := range procs {
		_ = p.Stop(closeStopGrace) //nolint:errcheck // Close cannot fail; removing the run below kills what the process record names.
	}
	r.cancelBuilds()
	r.builds.Wait()
	r.reaps.Wait()
	if degraded {
		r.warn(fmt.Sprintf("review-party could not confine Reviewer processes to a Job Object; descendants of Reviewers started under %s may remain; run 'review-party clean' to remove what is left", dir))
	}
	if dir == "" {
		return
	}
	r.warnReport(r.removeClaims([]claim{{dir: dir, lease: lease}}))
	r.restoreTemp()
}

func (r *Run) currentID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id
}

func (r *Run) warnReport(report Report) {
	for _, line := range report.Warnings() {
		r.warn(line)
	}
}

// ErrNoRun means the invocation has no run: Open failed and the command
// reached code that needs one.
var ErrNoRun = errors.New("review-party has no runtime directory for this invocation; see the earlier warning")

type runKey struct{}

// WithRun scopes r to one invocation.
func WithRun(ctx context.Context, r *Run) context.Context {
	return context.WithValue(ctx, runKey{}, r)
}

// From returns the invocation's run, or ErrNoRun.
func From(ctx context.Context) (*Run, error) {
	if r, ok := ctx.Value(runKey{}).(*Run); ok && r != nil {
		return r, nil
	}
	return nil, ErrNoRun
}
