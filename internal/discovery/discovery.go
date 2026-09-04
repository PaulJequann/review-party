// Package discovery owns bounded, observational model discovery for the
// Reviewer harnesses. It never writes configuration or starts authentication.
package discovery

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultDeadline bounds one passive discovery request.
	DefaultDeadline = 10 * time.Second

	// StatusSupported means the harness returned at least one model choice.
	StatusSupported Status = "supported"
	// StatusAuthenticationRequired means the harness explicitly reported that
	// authentication is needed before it can report models.
	StatusAuthenticationRequired Status = "authentication_required"
	// StatusUnavailable means the harness could not produce usable model
	// choices before its bounded deadline.
	StatusUnavailable Status = "unavailable"
	// StatusUnsupported means no safe observational interface is available.
	StatusUnsupported Status = "unsupported"

	AuthAvailable   AuthStatus = "available"
	AuthConfigured  AuthStatus = "configured"
	AuthRequired    AuthStatus = "required"
	AuthUnknown     AuthStatus = "unknown"
	AuthUnsupported AuthStatus = "unsupported"
	AuthUnavailable AuthStatus = "unavailable"
)

// Status is the result of one bounded discovery observation.
type Status string

// AuthStatus describes an observational authentication result. Available does
// not promise that a later paid request will succeed; it only means discovery
// did not report an authentication failure.
type AuthStatus string

// Model is one provider-reported model choice.
type Model struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name,omitempty"`
	Default          bool     `json:"default,omitempty"`
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
}

// SignInAction is a documented command the Caller may choose to run. The
// discovery module reports it but never executes it.
type SignInAction struct {
	Command          []string `json:"command"`
	Description      string   `json:"description"`
	DocumentationURL string   `json:"documentation_url,omitempty"`
}

// Authentication is the non-mutating authentication observation associated
// with a discovery result.
type Authentication struct {
	Status     AuthStatus    `json:"status"`
	Diagnostic string        `json:"diagnostic,omitempty"`
	SignIn     *SignInAction `json:"sign_in,omitempty"`
}

// Result is the stable machine-readable result of one discovery observation.
type Result struct {
	Reviewer       string         `json:"reviewer"`
	Status         Status         `json:"status"`
	Models         []Model        `json:"models,omitempty"`
	HarnessVersion string         `json:"harness_version,omitempty"`
	Authentication Authentication `json:"authentication"`
	ObservedAt     time.Time      `json:"observed_at"`
	Diagnostic     string         `json:"diagnostic,omitempty"`
	Cached         bool           `json:"cached,omitempty"`
}

// Observation is the adapter-facing portion of Result. The Service adds the
// reviewer identity, timestamp, cache state, and final status normalization.
type Observation struct {
	Status         Status
	Models         []Model
	ModelsComplete bool
	HarnessVersion string
	Authentication Authentication
	Diagnostic     string
}

// Adapter is the small seam for a Reviewer-specific discovery protocol.
// Adapters must return when ctx is cancelled. Discover invokes one adapter
// synchronously; DiscoverMany runs adapters concurrently and may return
// before a non-cooperative adapter finishes, so adapters own that cleanup
// contract at the boundary.
type Adapter interface {
	Reviewer() string
	Discover(context.Context) Observation
}

// Cache stores only disposable successful discovery material. Implementations
// must not treat cache bytes as configuration or as proof of current access.
// Entries expire, and Forget must remove one reviewer's entry.
type Cache interface {
	Load(reviewer string) (Result, bool, error)
	Save(reviewer string, result Result) error
	Forget(reviewer string) error
}

// Service runs bounded observations and provides immediate choices to Hub
// consumers while a live observation completes asynchronously.
type Service struct {
	adapters     map[string]Adapter
	cache        Cache
	cacheLocks   map[string]*sync.Mutex
	cacheLocksMu sync.Mutex
	cacheWrites  cacheWriteTracker
	forgetGens   map[string]*atomic.Uint64
	forgetMu     sync.Mutex
	deadline     time.Duration
	now          func() time.Time
}

// Options configures a discovery Service. A nil Cache disables persistence;
// a non-positive Deadline uses DefaultDeadline.
type Options struct {
	Adapters []Adapter
	Cache    Cache
	Deadline time.Duration
	Now      func() time.Time
}

// NewService constructs a Service over the supplied Reviewer adapters.
func NewService(options Options) *Service {
	deadline := options.Deadline
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	adapters := make(map[string]Adapter, len(options.Adapters))
	for _, adapter := range options.Adapters {
		if adapter == nil || strings.TrimSpace(adapter.Reviewer()) == "" {
			continue
		}
		adapters[adapter.Reviewer()] = adapter
	}
	return &Service{adapters: adapters, cache: options.Cache, cacheLocks: make(map[string]*sync.Mutex), forgetGens: make(map[string]*atomic.Uint64), deadline: deadline, now: now}
}

// NewDefaultService wires the four Reviewers currently supported by Review
// Party. Each adapter remains independent, so one unavailable harness does
// not prevent another from being observed.
func NewDefaultService() *Service {
	runner := NewDefaultRunner()
	return NewService(Options{
		Adapters: []Adapter{
			NewGrokAdapter(runner), NewOpenCodeAdapter(runner), NewCopilotAdapter(), NewCodexAdapter(),
		},
		Cache: DefaultCache(),
	})
}

// Reviewers returns known Reviewer IDs in stable order.
func (service *Service) Reviewers() []string {
	ids := make([]string, 0, len(service.adapters))
	for id := range service.adapters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Discover runs one bounded live observation. Authentication is observed only
// through the adapter's read-only protocol; no login command is launched.
func (service *Service) Discover(ctx context.Context, reviewer string) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	observationContext, cancel := context.WithTimeout(ctx, service.deadline)
	defer cancel()
	return service.discover(observationContext, reviewer)
}

func (service *Service) discover(ctx context.Context, reviewer string) Result {
	result := Result{Reviewer: reviewer, HarnessVersion: "unknown", ObservedAt: service.now().UTC()}
	if cause := ctx.Err(); cause != nil {
		return unavailableDiscoveryResult(reviewer, cause, result.ObservedAt)
	}
	adapter, found := service.adapters[reviewer]
	if !found {
		result.Status = StatusUnsupported
		result.Authentication = Authentication{Status: AuthUnsupported}
		result.Diagnostic = "no discovery adapter is registered"
		return result
	}
	observation := adapter.Discover(ctx)
	normalizeObservation(&result, observation, ctx)
	if service.shouldCache(result, ctx) {
		service.saveCachedResult(reviewer, result)
	}
	return result
}

func (service *Service) saveCachedResult(reviewer string, result Result) {
	cached := cacheableResult(result)
	cached.Models = cloneModels(result.Models)
	generation := service.forgetGeneration(reviewer).Load()
	service.cacheWrites.start()
	go func() {
		defer service.cacheWrites.finish()
		cacheLock := service.cacheLock(reviewer)
		cacheLock.Lock()
		defer cacheLock.Unlock()
		if service.forgetGeneration(reviewer).Load() != generation {
			return
		}
		current, found, err := service.cache.Load(reviewer)
		if newerCacheExists(current, found, err, cached.ObservedAt) {
			return
		}
		if err := service.cache.Save(reviewer, cached); err != nil {
			return
		}
	}()
}

// forgetGeneration returns the number of ForgetCached operations recorded for
// the Reviewer. Cache writers capture it when queued and re-check it under the
// per-Reviewer cache lock, so a forget that races a still-queued write wins:
// the outdated write skips its save instead of resurrecting the removed entry.
func (service *Service) forgetGeneration(reviewer string) *atomic.Uint64 {
	service.forgetMu.Lock()
	defer service.forgetMu.Unlock()
	generation, found := service.forgetGens[reviewer]
	if !found {
		generation = &atomic.Uint64{}
		service.forgetGens[reviewer] = generation
	}
	return generation
}

func (service *Service) shouldCache(result Result, ctx context.Context) bool {
	return result.Status == StatusSupported && service.cache != nil && ctx.Err() == nil
}

func newerCacheExists(current Result, found bool, err error, observedAt time.Time) bool {
	return err == nil && found && current.ObservedAt.After(observedAt)
}

// Flush waits for cache writes already queued by the Service. Cache
// persistence is best effort; a cancelled context leaves the writer running.
func (service *Service) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return service.cacheWrites.wait(ctx)
}

// DiscoverMany runs selected Reviewer observations concurrently and bounds the
// caller even if an adapter violates its cancellation contract. A nil list
// selects every registered Reviewer in stable order.
func (service *Service) DiscoverMany(ctx context.Context, reviewers []string) []Result {
	if ctx == nil {
		ctx = context.Background()
	}
	reviewers = service.selectedReviewers(reviewers)
	observationContext, cancel := context.WithTimeout(ctx, service.deadline)
	defer cancel()
	return service.collectDiscoveries(observationContext, reviewers)
}

type discoveryCompletion struct {
	index  int
	result Result
}

func (service *Service) selectedReviewers(reviewers []string) []string {
	if reviewers == nil {
		return service.Reviewers()
	}
	return append([]string(nil), reviewers...)
}

func (service *Service) collectDiscoveries(ctx context.Context, reviewers []string) []Result {
	results := make([]Result, len(reviewers))
	completed := make([]bool, len(reviewers))
	done := make(chan discoveryCompletion, len(reviewers))
	for index, reviewer := range reviewers {
		go func(index int, reviewer string) {
			done <- discoveryCompletion{index: index, result: service.discover(ctx, reviewer)}
		}(index, reviewer)
	}
	for received := 0; received < len(reviewers); received++ {
		select {
		case completion := <-done:
			results[completion.index] = completion.result
			completed[completion.index] = true
		case <-ctx.Done():
			return service.unavailableDiscoveries(reviewers, results, completed, ctx.Err())
		}
	}
	return results
}

func (service *Service) unavailableDiscoveries(reviewers []string, results []Result, completed []bool, cause error) []Result {
	for index, reviewer := range reviewers {
		if !completed[index] {
			results[index] = unavailableDiscoveryResult(reviewer, cause, service.now())
		}
	}
	return results
}

func unavailableDiscoveryResult(reviewer string, cause error, now time.Time) Result {
	diagnostic := "discovery deadline exceeded"
	if cause != nil {
		diagnostic = compactDiagnostic(cause.Error())
	}
	return Result{
		Reviewer: reviewer, Status: StatusUnavailable, HarnessVersion: "unknown",
		Authentication: Authentication{Status: AuthUnavailable}, ObservedAt: now.UTC(), Diagnostic: diagnostic,
	}
}

func normalizeObservation(result *Result, observation Observation, ctx context.Context) {
	result.Status = observation.Status
	result.Models = cloneModels(observation.Models)
	result.HarnessVersion = firstNonempty(observation.HarnessVersion, "unknown")
	result.Authentication = cloneAuthentication(observation.Authentication)
	result.Authentication.Diagnostic = compactDiagnostic(result.Authentication.Diagnostic)
	result.Diagnostic = compactDiagnostic(observation.Diagnostic)
	if observationTimedOut(ctx) && !observation.ModelsComplete {
		markObservationUnavailable(result, ctx)
	}
	if result.Status == StatusSupported && len(result.Models) == 0 {
		result.Status = StatusUnsupported
		result.Diagnostic = firstNonempty(result.Diagnostic, "discovery returned no models")
	}
	if result.Authentication.Status == "" {
		result.Authentication.Status = authenticationStatusFor(result.Status)
	}
}

func observationTimedOut(ctx context.Context) bool {
	return ctx.Err() != nil
}

func markObservationUnavailable(result *Result, ctx context.Context) {
	result.Status = StatusUnavailable
	result.Diagnostic = firstNonempty(result.Diagnostic, ctx.Err().Error())
	result.Authentication.Status = AuthUnavailable
}

func authenticationStatusFor(status Status) AuthStatus {
	switch status {
	case StatusSupported:
		return AuthAvailable
	case StatusUnavailable:
		return AuthUnavailable
	case StatusUnsupported:
		return AuthUnsupported
	case StatusAuthenticationRequired:
		return AuthRequired
	default:
		return AuthUnknown
	}
}

// Cached returns the last successful result without launching a harness.
func (service *Service) Cached(reviewer string) (Result, bool) {
	if service.cache == nil {
		return Result{}, false
	}
	cacheLock := service.cacheLock(reviewer)
	cacheLock.Lock()
	defer cacheLock.Unlock()
	result, found, err := service.cache.Load(reviewer)
	if !validCachedResult(result, found, err) {
		return Result{}, false
	}
	result.Models = cloneModels(result.Models)
	result.Cached = true
	return result, true
}

func (service *Service) cacheLock(reviewer string) *sync.Mutex {
	service.cacheLocksMu.Lock()
	defer service.cacheLocksMu.Unlock()
	if lock := service.cacheLocks[reviewer]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	service.cacheLocks[reviewer] = lock
	return lock
}

func validCachedResult(result Result, found bool, err error) bool {
	return err == nil && found && result.Status == StatusSupported && len(result.Models) > 0
}

// ForgetCached removes the reviewer's cached result so the next live
// observation rebuilds it. It reports whether cache material was discarded;
// a missing or disabled cache is not an error. A write still queued from an
// earlier discovery is invalidated, so it cannot resurrect the removed entry.
func (service *Service) ForgetCached(reviewer string) (bool, error) {
	if service.cache == nil {
		return false, nil
	}
	// The generation is bumped before taking the per-Reviewer lock: a write
	// that is queued but has not acquired the lock yet must observe the bump
	// no matter which of the two wins the lock. Bumping only alongside the
	// removal would let such a write pass its staleness check and save.
	service.forgetGeneration(reviewer).Add(1)
	cacheLock := service.cacheLock(reviewer)
	cacheLock.Lock()
	defer cacheLock.Unlock()
	if err := service.cache.Forget(reviewer); err != nil {
		return false, err
	}
	return true, nil
}

func cacheableResult(result Result) Result {
	result.Cached = false
	result.Diagnostic = ""
	result.Authentication = Authentication{}
	return result
}

func cloneAuthentication(authentication Authentication) Authentication {
	if authentication.SignIn != nil {
		clone := *authentication.SignIn
		clone.Command = append([]string(nil), clone.Command...)
		authentication.SignIn = &clone
	}
	return authentication
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
