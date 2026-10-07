package hostrun

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
)

// ViewSource is a Subject that can materialize itself into a directory.
type ViewSource interface {
	// ViewKey identifies the content; equal keys share one view per run.
	ViewKey() string
	// BuildView fills dir, which exists and is empty. The context is the
	// run's, cancelled by Close, not the requesting caller's.
	BuildView(ctx context.Context, dir string) error
}

type viewEntry struct {
	dir  string
	done chan struct{}
	err  error
}

// View returns the directory holding source's content, building it once per
// run per key. Concurrent calls for one key wait on a single build. A caller
// whose context ends while waiting gets its context error; the build keeps
// going for the others.
func (r *Run) View(ctx context.Context, source ViewSource) (string, error) {
	dir, err := r.ensureDir()
	if err != nil {
		return "", err
	}
	entry, err := r.viewFor(dir, source)
	if err != nil {
		return "", err
	}
	select {
	case <-entry.done:
		return entry.dir, entry.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *Run) viewFor(runDir string, source ViewSource) (*viewEntry, error) {
	key := source.ViewKey()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errRunClosed
	}
	if entry, ok := r.views[key]; ok {
		return entry, nil
	}
	views := filepath.Join(runDir, viewsName)
	if err := os.MkdirAll(views, 0o700); err != nil {
		return nil, err
	}
	r.viewCount++
	entry := &viewEntry{dir: filepath.Join(views, strconv.Itoa(r.viewCount)), done: make(chan struct{})}
	if err := os.Mkdir(entry.dir, 0o700); err != nil {
		return nil, err
	}
	r.views[key] = entry
	r.builds.Add(1)
	go r.build(key, entry, source)
	return entry, nil
}

func (r *Run) build(key string, entry *viewEntry, source ViewSource) {
	defer r.builds.Done()
	defer close(entry.done)
	entry.err = source.BuildView(r.buildCtx, entry.dir)
	if entry.err == nil {
		return
	}
	r.mu.Lock()
	delete(r.views, key)
	r.mu.Unlock()
	_ = os.RemoveAll(entry.dir) //nolint:errcheck // A partial view that survives goes with the run directory.
}
