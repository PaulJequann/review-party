package hostrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fileSource struct {
	key  string
	name string
}

func (s fileSource) ViewKey() string { return s.key }

func (s fileSource) BuildView(_ context.Context, dir string) error {
	return os.WriteFile(filepath.Join(dir, s.name), []byte(s.key), 0o600)
}

type gatedSource struct {
	builds  atomic.Int32
	release chan struct{}
	fail    atomic.Bool
}

var errBuild = errors.New("build failed")

func newGatedSource() *gatedSource {
	return &gatedSource{release: make(chan struct{})}
}

func (s *gatedSource) ViewKey() string { return "gated" }

func (s *gatedSource) BuildView(ctx context.Context, _ string) error {
	s.builds.Add(1)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.fail.Load() {
		return errBuild
	}
	return nil
}

func TestViewBuildsOncePerKeyAcrossConcurrentCallers(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	source := newGatedSource()
	const callers = 3
	dirs := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { dirs[i], errs[i] = r.View(context.Background(), source) })
	}
	waitFor(t, time.Second, "the first build to start", func() bool { return source.builds.Load() == 1 })
	close(source.release)
	wg.Wait()
	for i := range callers {
		if errs[i] != nil || dirs[i] != dirs[0] || dirs[0] == "" {
			t.Fatalf("caller %d got %q, %v; want %q and no error", i, dirs[i], errs[i], dirs[0])
		}
	}
	again, err := r.View(context.Background(), source)
	if err != nil || again != dirs[0] {
		t.Fatalf("second View = %q, %v; want the memoized %q", again, err, dirs[0])
	}
	if builds := source.builds.Load(); builds != 1 {
		t.Fatalf("%d builds for one key; want 1", builds)
	}
	if !fileExists(dirs[0]) {
		t.Fatalf("view directory %s does not exist", dirs[0])
	}
}

func TestViewFailureIsNotMemoized(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	source := newGatedSource()
	source.fail.Store(true)
	close(source.release)
	if _, err := r.View(context.Background(), source); !errors.Is(err, errBuild) {
		t.Fatalf("failed build returned %v; want %v", err, errBuild)
	}
	views := filepath.Join(r.dir, viewsName)
	if names := rootNames(t, views); len(names) != 0 {
		t.Fatalf("failed build left %v under v/", names)
	}

	source.fail.Store(false)
	dir, err := r.View(context.Background(), source)
	if err != nil {
		t.Fatalf("rebuild after failure: %v", err)
	}
	if builds := source.builds.Load(); builds != 2 {
		t.Fatalf("%d builds; want a rebuild after the failure", builds)
	}
	if filepath.Dir(dir) != views || !fileExists(dir) {
		t.Fatalf("rebuilt view %q is not a directory under %s", dir, views)
	}
}

func TestViewWaiterKeepsItsOwnContextAndCloseCancelsBuilds(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	source := newGatedSource()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.View(ctx, source)
		done <- err
	}()
	waitFor(t, time.Second, "the build to start", func() bool { return source.builds.Load() == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter got %v; want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter did not return")
	}

	closed := make(chan struct{})
	go func() {
		r.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the running build")
	}
	if builds := source.builds.Load(); builds != 1 {
		t.Fatalf("%d builds; want the single cancelled one", builds)
	}
}
