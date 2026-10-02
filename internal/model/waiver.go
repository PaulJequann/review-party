package model

import "time"

type WaiverID string

// WaivedBy records how a Checkpoint Waiver was confirmed.
type WaivedBy string

const (
	WaivedByTerminal       WaivedBy = "terminal"
	WaivedByNonInteractive WaivedBy = "non-interactive"
)

// WaiverKey identifies the exact Checkpoint change a Waiver applies to: the
// Checkpoint name and the ContentChangesDigest of its non-exempt set.
type WaiverKey struct {
	Checkpoint    string `json:"checkpoint"`
	ContentDigest string `json:"content_digest"`
}

// CheckpointWaiver records that one exact content change passed a Checkpoint
// without Coverage, in the repository root it was recorded from.
type CheckpointWaiver struct {
	ID         WaiverID  `json:"id"`
	Key        WaiverKey `json:"key"`
	Repository string    `json:"repository"`
	Reason     string    `json:"reason"`
	WaivedBy   WaivedBy  `json:"waived_by"`
	CreatedAt  time.Time `json:"created_at"`
}
