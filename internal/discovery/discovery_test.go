package discovery

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeAdapter struct {
	id          string
	observation Observation
	started     chan struct{}
	release     chan struct{}
	finished    chan struct{}
}

func (adapter fakeAdapter) Reviewer() string { return adapter.id }

func (adapter fakeAdapter) Discover(context.Context) Observation {
	if adapter.finished != nil {
		defer close(adapter.finished)
	}
	if adapter.started != nil {
		close(adapter.started)
	}
	if adapter.release != nil {
		<-adapter.release
	}
	return adapter.observation
}

type fakeCache struct {
	result    Result
	found     bool
	saved     []Result
	savedDone chan struct{}
	mu        sync.Mutex
}

func (cache *fakeCache) Load(string) (Result, bool, error) { return cache.result, cache.found, nil }

func (cache *fakeCache) Save(_ string, result Result) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.saved = append(cache.saved, result)
	if cache.savedDone != nil {
		close(cache.savedDone)
	}
	return nil
}

func TestOpenReturnsKnownChoicesBeforeLiveDiscoveryCompletes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	cache := &fakeCache{found: true, savedDone: make(chan struct{}), result: Result{
		Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "cached/model"}},
	}}
	service := NewService(Options{
		Adapters: []Adapter{fakeAdapter{
			id: "grok", started: started, release: release,
			observation: Observation{Status: StatusSupported, Models: []Model{{ID: "live/model"}}},
		}},
		Cache: cache, Now: func() time.Time { return time.Unix(10, 0) },
	})

	session := service.Open(context.Background(), ChoiceRequest{
		Reviewer: "grok", Configured: []string{"configured/model"}, Packaged: []string{"packaged/model"},
	})
	want := []ModelChoice{
		{Model: Model{ID: "cached/model"}, Sources: []ChoiceSource{ChoiceSourceCached}},
		{Model: Model{ID: "configured/model"}, Sources: []ChoiceSource{ChoiceSourceConfigured}},
		{Model: Model{ID: "packaged/model"}, Sources: []ChoiceSource{ChoiceSourcePackaged}},
	}
	if !reflect.DeepEqual(session.Choices, want) {
		t.Fatalf("immediate choices = %#v, want %#v", session.Choices, want)
	}
	waitForDiscoverySignal(t, started, "live discovery did not start")
	close(release)
	refresh := waitForDiscoveryResult(t, session.Refresh)
	if refresh.Status != StatusSupported {
		t.Fatalf("refresh status = %s, want supported", refresh.Status)
	}
	if len(refresh.Models) != 1 || refresh.Models[0].ID != "live/model" {
		t.Fatalf("refresh models = %#v, want live/model", refresh.Models)
	}
	waitForDiscoverySignal(t, cache.savedDone, "successful discovery was not cached")
	if len(cache.saved) != 1 {
		t.Fatalf("saved cache results = %#v", cache.saved)
	}
	if cache.saved[0].Cached {
		t.Fatalf("saved cache result was marked cached: %#v", cache.saved[0])
	}
}

func TestOpenCanCancelLiveDiscovery(t *testing.T) {
	started := make(chan struct{})
	service := NewService(Options{Adapters: []Adapter{cancelAwareAdapter{id: "grok", started: started}}})
	session := service.Open(context.Background(), ChoiceRequest{Reviewer: "grok"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("live discovery did not start")
	}
	session.Close()
	select {
	case result, ok := <-session.Refresh:
		if !ok || result.Status != StatusUnavailable {
			t.Fatalf("refresh after close = %#v, open = %v", result, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("closed discovery session did not finish")
	}
	if _, ok := <-session.Refresh; ok {
		t.Fatal("closed discovery session left its refresh channel open")
	}
}

func TestOpenBoundsAnUncooperativeAdapter(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	service := NewService(Options{Deadline: 10 * time.Millisecond, Adapters: []Adapter{fakeAdapter{
		id: "grok", started: started, release: release,
		observation: Observation{Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}},
	}}})
	session := service.Open(context.Background(), ChoiceRequest{Reviewer: "grok"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("live discovery did not start")
	}
	select {
	case result, ok := <-session.Refresh:
		if !ok || result.Status != StatusUnavailable {
			t.Fatalf("refresh = %#v, open = %v", result, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("open waited past the discovery deadline")
	}
	close(release)
}

func TestConcurrentCacheWritesForSameReviewerAreSerialized(t *testing.T) {
	cache := &trackingCache{savedDone: make(chan struct{})}
	service := NewService(Options{
		Adapters: []Adapter{fakeAdapter{
			id: "grok", observation: Observation{Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}},
		}},
		Cache: cache,
	})
	go service.Discover(context.Background(), "grok")
	go service.Discover(context.Background(), "grok")
	select {
	case <-cache.savedDone:
	case <-time.After(time.Second):
		t.Fatal("successful discoveries were not cached")
	}
	if cache.maxActive != 1 {
		t.Fatalf("concurrent cache writes = %d, want 1", cache.maxActive)
	}
}

func TestDiscoverReturnsBeforeSlowCacheWrite(t *testing.T) {
	cache := &blockingCache{started: make(chan struct{}), release: make(chan struct{})}
	service := NewService(Options{Adapters: []Adapter{fakeAdapter{
		id: "grok", observation: Observation{Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}},
	}}, Cache: cache})
	resultChannel := make(chan Result, 1)
	go func() { resultChannel <- service.Discover(context.Background(), "grok") }()
	select {
	case <-cache.started:
	case <-time.After(time.Second):
		t.Fatal("cache write did not start")
	}
	select {
	case result := <-resultChannel:
		if result.Status != StatusSupported {
			t.Fatalf("result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery waited for a slow cache write")
	}
	close(cache.release)
	if err := service.Flush(context.Background()); err != nil {
		t.Fatalf("flush error = %v", err)
	}
}

func TestSlowCacheWriteDoesNotBlockAnotherReviewerCacheRead(t *testing.T) {
	cache := &blockingCache{started: make(chan struct{}), release: make(chan struct{})}
	service := NewService(Options{Adapters: []Adapter{fakeAdapter{
		id: "grok", observation: Observation{Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}},
	}}, Cache: cache})
	go service.Discover(context.Background(), "grok")
	select {
	case <-cache.started:
	case <-time.After(time.Second):
		t.Fatal("cache write did not start")
	}
	readDone := make(chan struct{})
	go func() {
		service.Cached("opencode")
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("slow cache write blocked another Reviewer cache read")
	}
	close(cache.release)
	if err := service.Flush(context.Background()); err != nil {
		t.Fatalf("flush error = %v", err)
	}
}

type trackingCache struct {
	mu        sync.Mutex
	active    int
	maxActive int
	saved     int
	savedDone chan struct{}
}

type blockingCache struct {
	started chan struct{}
	release chan struct{}
}

func (cache *blockingCache) Load(string) (Result, bool, error) { return Result{}, false, nil }

func (cache *blockingCache) Save(string, Result) error {
	close(cache.started)
	<-cache.release
	return nil
}

func (cache *trackingCache) Load(string) (Result, bool, error) { return Result{}, false, nil }

func (cache *trackingCache) Save(string, Result) error {
	cache.mu.Lock()
	cache.active++
	if cache.active > cache.maxActive {
		cache.maxActive = cache.active
	}
	cache.mu.Unlock()
	time.Sleep(10 * time.Millisecond)
	cache.mu.Lock()
	cache.active--
	cache.saved++
	if cache.savedDone != nil && cache.saved == 2 {
		close(cache.savedDone)
	}
	cache.mu.Unlock()
	return nil
}

type cancelAwareAdapter struct {
	id      string
	started chan struct{}
}

func (adapter cancelAwareAdapter) Reviewer() string { return adapter.id }

func (adapter cancelAwareAdapter) Discover(ctx context.Context) Observation {
	close(adapter.started)
	<-ctx.Done()
	return Observation{Status: StatusUnavailable, Diagnostic: ctx.Err().Error()}
}

func TestMergeChoicesPreservesOrderAndMetadata(t *testing.T) {
	choices := MergeChoices(nil, []Model{{ID: "model", DisplayName: "Old"}}, ChoiceSourcePackaged)
	choices = MergeChoices(choices, []Model{{ID: "model", DisplayName: "Display", Default: true, ReasoningEfforts: []string{"low", "high"}}, {ID: "other"}}, ChoiceSourceDiscovered)
	want := []ModelChoice{
		{Model: Model{ID: "model", DisplayName: "Old", Default: true, ReasoningEfforts: []string{"low", "high"}}, Sources: []ChoiceSource{ChoiceSourcePackaged, ChoiceSourceDiscovered}},
		{Model: Model{ID: "other"}, Sources: []ChoiceSource{ChoiceSourceDiscovered}},
	}
	if !reflect.DeepEqual(choices, want) {
		t.Fatalf("choices = %#v, want %#v", choices, want)
	}
}

func TestDiscoveryNormalizesEmptySupportedResult(t *testing.T) {
	service := NewService(Options{Adapters: []Adapter{fakeAdapter{
		id: "grok", observation: Observation{Status: StatusSupported},
	}}})
	result := service.Discover(context.Background(), "grok")
	if result.Status != StatusUnsupported || result.Diagnostic != "discovery returned no models" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDiscoveryDoesNotKnowUnknownReviewer(t *testing.T) {
	result := NewService(Options{}).Discover(context.Background(), "unknown")
	if result.Status != StatusUnsupported || result.Authentication.Status != AuthUnsupported {
		t.Fatalf("result = %#v", result)
	}
}

func TestDiscoverManyBoundsAnUncooperativeAdapter(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	cache := &fakeCache{savedDone: make(chan struct{})}
	service := NewService(Options{Deadline: 10 * time.Millisecond, Adapters: []Adapter{fakeAdapter{
		id: "grok", started: started, release: release,
		finished:    finished,
		observation: Observation{Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}},
	}}, Cache: cache})
	resultsChannel := make(chan []Result, 1)
	go func() { resultsChannel <- service.DiscoverMany(context.Background(), nil) }()
	waitForDiscoverySignal(t, started, "uncooperative adapter did not start")
	startedAt := time.Now()
	results := <-resultsChannel
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("DiscoverMany waited %s for an uncooperative adapter", elapsed)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v, want one result", results)
	}
	if results[0].Status != StatusUnavailable {
		t.Fatalf("results = %#v", results)
	}
	close(release)
	waitForDiscoverySignal(t, finished, "uncooperative adapter did not finish after release")
	assertNoDiscoverySignal(t, cache.savedDone, 50*time.Millisecond, "late timed-out observation was cached")
}

func TestDiscoverManyPreservesCompletedResultsWhenAnotherAdapterTimesOut(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	service := NewService(Options{Deadline: 10 * time.Millisecond, Adapters: []Adapter{
		fakeAdapter{id: "fast", observation: Observation{Status: StatusSupported, Models: []Model{{ID: "fast-model"}}}},
		fakeAdapter{id: "slow", started: started, release: release, observation: Observation{Status: StatusSupported, Models: []Model{{ID: "slow-model"}}}},
	}})
	results := make(chan []Result, 1)
	go func() { results <- service.DiscoverMany(context.Background(), []string{"fast", "slow"}) }()
	waitForDiscoverySignal(t, started, "slow adapter did not start")
	observations := <-results
	if observations[0].Status != StatusSupported || observations[0].Reviewer != "fast" {
		t.Fatalf("completed result = %#v", observations[0])
	}
	if observations[1].Status != StatusUnavailable || observations[1].Reviewer != "slow" {
		t.Fatalf("timed-out result = %#v", observations[1])
	}
	close(release)
}

func waitForDiscoverySignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func waitForDiscoveryResult(t *testing.T, results <-chan Result) Result {
	t.Helper()
	select {
	case result, ok := <-results:
		if !ok {
			t.Fatal("discovery refresh channel closed without a result")
		}
		return result
	case <-time.After(time.Second):
		t.Fatal("discovery refresh did not arrive")
		return Result{}
	}
}

func assertNoDiscoverySignal(t *testing.T, signal <-chan struct{}, wait time.Duration, message string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(message)
	case <-time.After(wait):
	}
}

func TestLateCompletedObservationIsNotCached(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	cache := &fakeCache{}
	service := NewService(Options{Deadline: 10 * time.Millisecond, Adapters: []Adapter{fakeAdapter{
		id: "grok", started: started, release: release, finished: finished,
		observation: Observation{Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}, ModelsComplete: true},
	}}, Cache: cache})
	results := service.DiscoverMany(context.Background(), nil)
	if len(results) != 1 || results[0].Status != StatusUnavailable {
		t.Fatalf("results = %#v", results)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("late adapter did not finish")
	}
	if err := service.Flush(context.Background()); err != nil {
		t.Fatalf("flush error = %v", err)
	}
	if len(cache.saved) != 0 {
		t.Fatalf("late observation was cached: %#v", cache.saved)
	}
}

func TestCacheWriteSkipsOlderObservation(t *testing.T) {
	newer := Result{Reviewer: "grok", Status: StatusSupported, ObservedAt: time.Unix(20, 0)}
	cache := &fakeCache{found: true, result: newer}
	service := NewService(Options{Cache: cache})
	service.saveCachedResult("grok", Result{Reviewer: "grok", Status: StatusSupported, ObservedAt: time.Unix(10, 0), Models: []Model{{ID: "old"}}})
	if err := service.Flush(context.Background()); err != nil {
		t.Fatalf("flush error = %v", err)
	}
	if len(cache.saved) != 0 {
		t.Fatalf("older observation was cached: %#v", cache.saved)
	}
}
