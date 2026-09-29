package model

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type MissID string

type MissSource string

const (
	MissSourceCodexPR  MissSource = "codex-pr"
	MissSourceHuman    MissSource = "human"
	MissSourceIncident MissSource = "incident"
	MissSourceOther    MissSource = "other"
)

var missSources = []MissSource{MissSourceCodexPR, MissSourceHuman, MissSourceIncident, MissSourceOther}

func ParseMissSource(text string) (MissSource, error) {
	for _, source := range missSources {
		if string(source) == text {
			return source, nil
		}
	}
	return "", fmt.Errorf("unsupported miss source %q; expected codex-pr, human, incident, or other", text)
}

func ParseMissPath(text string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(text))
	if cleaned == "." || !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("miss path %q must be a repository-relative path inside the repository", text)
	}
	return filepath.ToSlash(cleaned), nil
}

type MissLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

func (location MissLocation) String() string {
	if location.Line > 0 {
		return fmt.Sprintf("%s:%d", location.Path, location.Line)
	}
	return location.Path
}

type MissRemoval struct {
	Reason    string    `json:"reason"`
	RemovedBy string    `json:"removed_by"`
	RemovedAt time.Time `json:"removed_at"`
}

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
