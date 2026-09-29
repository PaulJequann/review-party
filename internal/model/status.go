package model

import "time"

type ReviewStatusKind string

const (
	ReviewStatusBundle ReviewStatusKind = "bundle"
	ReviewStatusReview ReviewStatusKind = "review"
)

// ReviewStatus is a non-blocking ledger snapshot of one Review Bundle or one
// Review. Reviews lists a Review Bundle's members in selection order, or the
// single Review itself.
type ReviewStatus struct {
	ID          string               `json:"id"`
	Kind        ReviewStatusKind     `json:"kind"`
	Lifecycle   Lifecycle            `json:"lifecycle"`
	Repository  string               `json:"repository"`
	Termination *BundleTermination   `json:"termination,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	Reviews     []ReviewStatusMember `json:"reviews"`
}

type ReviewStatusMember struct {
	ReviewID     ReviewID           `json:"review_id"`
	Scope        string             `json:"scope,omitempty"`
	Profile      string             `json:"profile"`
	Reviewer     string             `json:"reviewer"`
	Model        string             `json:"model,omitempty"`
	Lifecycle    Lifecycle          `json:"lifecycle"`
	Attempts     int                `json:"attempts"`
	Status       string             `json:"status,omitempty"`
	FindingCount int                `json:"finding_count"`
	Termination  *ReviewTermination `json:"termination,omitempty"`
	DurationMS   int64              `json:"duration_ms,omitempty"`
	UpdatedAt    time.Time          `json:"updated_at"`
	ReadError    string             `json:"read_error,omitempty"`
}

func (lifecycle Lifecycle) Terminal() bool {
	return lifecycle == LifecycleCompleted || lifecycle == LifecycleIncomplete
}
