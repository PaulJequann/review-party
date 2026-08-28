// Package discovery owns bounded, observational model discovery for the
// Reviewer harnesses. It never writes configuration or starts authentication.
package discovery

import (
	"context"
	"sort"
	"strings"
	"sync"
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
type Cache interface {
	Load(reviewer string) (Result, bool, error)
	Save(reviewer string, result Result) error
}

// Service runs bounded observations and provides immediate choices to Hub
// consumers while a live observation completes asynchronously.
type Service struct {
	adapters    map[string]Adapter
	cache       Cache
	cacheMu     sync.Mutex
	cacheWrites cacheWriteTracker
	deadline    time.Duration
	now         func() time.Time
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
	return &Service{adapters: adapters, cache: options.Cache, deadline: deadline, now: now}
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
	result := Result{Reviewer: reviewer, HarnessVersion: "unknown", ObservedAt: service.now().UTC()}
	adapter, found := service.adapters[reviewer]
	if !found {
		result.Status = StatusUnsupported
		result.Authentication = Authentication{Status: AuthUnsupported}
		result.Diagnostic = "no discovery adapter is registered"
		return result
	}
	observationContext, cancel := context.WithTimeout(ctx, service.deadline)
	defer cancel()
	observation := adapter.Discover(observationContext)
	normalizeObservation(&result, observation, observationContext)
	if service.shouldCache(result, observationContext) {
		service.saveCachedResult(reviewer, result)
	}
	return result
}

func (service *Service) saveCachedResult(reviewer string, result Result) {
	cached := cacheableResult(result)
	cached.Models = cloneModels(result.Models)
	service.cacheWrites.start()
	go func() {
		defer service.cacheWrites.finish()
		service.cacheMu.Lock()
		defer service.cacheMu.Unlock()
		current, found, err := service.cache.Load(reviewer)
		if newerCacheExists(current, found, err, cached.ObservedAt) {
			return
		}
		_ = service.cache.Save(reviewer, cached)
	}()
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

type cacheWriteTracker struct {
	mu      sync.Mutex
	pending int
	done    chan struct{}
}

func (tracker *cacheWriteTracker) start() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.pending == 0 {
		tracker.done = make(chan struct{})
	}
	tracker.pending++
}

func (tracker *cacheWriteTracker) finish() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.pending--
	if tracker.pending == 0 {
		close(tracker.done)
	}
}

func (tracker *cacheWriteTracker) wait(ctx context.Context) error {
	tracker.mu.Lock()
	done := tracker.done
	tracker.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
			done <- discoveryCompletion{index: index, result: service.Discover(ctx, reviewer)}
		}(index, reviewer)
	}
	for received := 0; received < len(reviewers); received++ {
		select {
		case completion := <-done:
			results[completion.index] = completion.result
			completed[completion.index] = true
		case <-ctx.Done():
			return service.unavailableDiscoveries(reviewers, completed, ctx.Err())
		}
	}
	return results
}

func (service *Service) unavailableDiscoveries(reviewers []string, completed []bool, cause error) []Result {
	results := make([]Result, len(reviewers))
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
	service.cacheMu.Lock()
	defer service.cacheMu.Unlock()
	result, found, err := service.cache.Load(reviewer)
	if !validCachedResult(result, found, err) {
		return Result{}, false
	}
	result.Models = cloneModels(result.Models)
	result.Cached = true
	return result, true
}

func validCachedResult(result Result, found bool, err error) bool {
	return err == nil && found && result.Status == StatusSupported && len(result.Models) > 0
}

// ChoiceSource explains why a model is immediately available to a consumer.
type ChoiceSource string

const (
	ChoiceSourceCached     ChoiceSource = "cached"
	ChoiceSourceConfigured ChoiceSource = "configured"
	ChoiceSourcePackaged   ChoiceSource = "packaged"
	ChoiceSourceDiscovered ChoiceSource = "discovered"
)

// ModelChoice is a deduplicated picker entry with all known provenance.
type ModelChoice struct {
	Model   Model          `json:"model"`
	Sources []ChoiceSource `json:"sources"`
}

// ChoiceRequest supplies non-discovery choices that must remain usable while
// a live harness observation is pending.
type ChoiceRequest struct {
	Reviewer   string
	Configured []string
	Packaged   []string
}

// ChoiceSnapshot is the canonical immediate model-choice view. It preserves
// source provenance and answers exact-ID membership without starting discovery.
type ChoiceSnapshot struct {
	choices []ModelChoice
}

// ChoiceSnapshot builds cached, configured, and packaged choices in display
// order. The returned snapshot is independent of later source mutations.
func (service *Service) ChoiceSnapshot(request ChoiceRequest) ChoiceSnapshot {
	choices := []ModelChoice{}
	if cached, found := service.Cached(request.Reviewer); found {
		choices = MergeChoices(choices, cached.Models, ChoiceSourceCached)
	}
	choices = MergeChoices(choices, stringModels(request.Configured), ChoiceSourceConfigured)
	choices = MergeChoices(choices, stringModels(request.Packaged), ChoiceSourcePackaged)
	return ChoiceSnapshot{choices: choices}
}

// Choices returns the snapshot's choices with copied mutable fields.
func (snapshot ChoiceSnapshot) Choices() []ModelChoice {
	return cloneChoices(snapshot.choices)
}

// Contains reports whether the exact model ID appears in the snapshot.
func (snapshot ChoiceSnapshot) Contains(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, choice := range snapshot.choices {
		if choice.Model.ID == model {
			return true
		}
	}
	return false
}

// ChoiceSession is returned without waiting for live discovery. Refresh is a
// one-result buffered channel and closes after the observation is delivered.
type ChoiceSession struct {
	Choices []ModelChoice
	Refresh <-chan Result
	cancel  context.CancelFunc
}

// Open builds immediate choices from cached, configured, and packaged models,
// then starts a bounded live observation in the background. Call Close when
// the consumer abandons the picker before the refresh arrives.
func (service *Service) Open(ctx context.Context, request ChoiceRequest) ChoiceSession {
	if ctx == nil {
		ctx = context.Background()
	}
	choices := service.ChoiceSnapshot(request).Choices()
	refreshContext, cancel := context.WithCancel(ctx)
	refresh := make(chan Result, 1)
	go func() {
		defer close(refresh)
		results := service.DiscoverMany(refreshContext, []string{request.Reviewer})
		refresh <- results[0]
	}()
	return ChoiceSession{Choices: choices, Refresh: refresh, cancel: cancel}
}

// Close abandons the live refresh and releases any adapter resources owned by
// the session. The refresh channel closes after the adapter returns.
func (session ChoiceSession) Close() {
	if session.cancel != nil {
		session.cancel()
	}
}

// MergeChoices adds models while preserving first-seen order and records each
// source once. It is safe for Hub code to call again with the live result.
func MergeChoices(existing []ModelChoice, models []Model, source ChoiceSource) []ModelChoice {
	choices := cloneChoices(existing)
	indexes := make(map[string]int, len(choices))
	for index, choice := range choices {
		indexes[choice.Model.ID] = index
	}
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			continue
		}
		index, found := indexes[model.ID]
		if !found {
			choices = append(choices, ModelChoice{Model: cloneModel(model), Sources: []ChoiceSource{source}})
			indexes[model.ID] = len(choices) - 1
			continue
		}
		choices[index].Model = mergeModelMetadata(choices[index].Model, model)
		if !containsSource(choices[index].Sources, source) {
			choices[index].Sources = append(choices[index].Sources, source)
		}
	}
	return choices
}

func cacheableResult(result Result) Result {
	result.Cached = false
	result.Diagnostic = ""
	result.Authentication = Authentication{}
	return result
}

func cloneModels(models []Model) []Model {
	result := make([]Model, len(models))
	for index, model := range models {
		result[index] = cloneModel(model)
	}
	return result
}

func cloneModel(model Model) Model {
	model.ReasoningEfforts = append([]string(nil), model.ReasoningEfforts...)
	return model
}

func cloneAuthentication(authentication Authentication) Authentication {
	if authentication.SignIn != nil {
		clone := *authentication.SignIn
		clone.Command = append([]string(nil), clone.Command...)
		authentication.SignIn = &clone
	}
	return authentication
}

func cloneChoices(choices []ModelChoice) []ModelChoice {
	result := make([]ModelChoice, len(choices))
	for index, choice := range choices {
		result[index] = ModelChoice{Model: cloneModel(choice.Model), Sources: append([]ChoiceSource(nil), choice.Sources...)}
	}
	return result
}

func mergeModelMetadata(current, incoming Model) Model {
	if current.DisplayName == "" {
		current.DisplayName = incoming.DisplayName
	}
	if !current.Default {
		current.Default = incoming.Default
	}
	if len(current.ReasoningEfforts) == 0 {
		current.ReasoningEfforts = append([]string(nil), incoming.ReasoningEfforts...)
	}
	return current
}

func containsSource(sources []ChoiceSource, wanted ChoiceSource) bool {
	for _, source := range sources {
		if source == wanted {
			return true
		}
	}
	return false
}

func stringModels(values []string) []Model {
	models := make([]Model, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			models = append(models, Model{ID: strings.TrimSpace(value)})
		}
	}
	return models
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
