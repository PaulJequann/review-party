package model

import (
	"fmt"
	"time"
)

// MissID identifies one recorded miss: a bug a completed review should have caught.
type MissID string

type MissSource string

const (
	MissSourceCodexPR  MissSource = "codex-pr"
	MissSourceHuman    MissSource = "human"
	MissSourceIncident MissSource = "incident"
	MissSourceOther    MissSource = "other"
)

var missSources = []MissSource{MissSourceCodexPR, MissSourceHuman, MissSourceIncident, MissSourceOther}

// ParseMissSource accepts only the closed set of miss sources.
func ParseMissSource(text string) (MissSource, error) {
	for _, source := range missSources {
		if string(source) == text {
			return source, nil
		}
	}
	return "", fmt.Errorf("unsupported miss source %q; expected codex-pr, human, incident, or other", text)
}

// MissLocation names where the missed bug lives. A zero Line means the whole file.
type MissLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type MissRemoval struct {
	Reason    string    `json:"reason"`
	RemovedBy string    `json:"removed_by"`
	RemovedAt time.Time `json:"removed_at"`
}

// MissReport is what a caller says about a miss. Review-derived facts such as
// repository, subject, and profile are deliberately absent: they come from the
// review record, never from the caller.
type MissReport struct {
	Location    MissLocation
	Source      MissSource
	Description string
	RecordedBy  string
}

type Miss struct {
	ID              MissID       `json:"id"`
	ReviewID        ReviewID     `json:"review_id"`
	Repository      string       `json:"repository"`
	SubjectKind     SubjectKind  `json:"subject_kind"`
	SubjectIdentity string       `json:"subject_identity"`
	Profile         string       `json:"profile"`
	Location        MissLocation `json:"location"`
	Source          MissSource   `json:"source"`
	Description     string       `json:"description"`
	RecordedBy      string       `json:"recorded_by"`
	RecordedAt      time.Time    `json:"recorded_at"`
	Removal         *MissRemoval `json:"removal,omitempty"`
}
