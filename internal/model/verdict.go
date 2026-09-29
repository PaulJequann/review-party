package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Verdict is a Caller's judgment of one Finding.
type Verdict string

const (
	VerdictAccepted Verdict = "accepted"
	VerdictRejected Verdict = "rejected"
	VerdictDeferred Verdict = "deferred"
)

// verdictVerbs pairs each Verdict with the verb a Caller types to record it.
var verdictVerbs = []struct {
	verdict Verdict
	verb    string
}{
	{VerdictAccepted, "accept"},
	{VerdictRejected, "reject"},
	{VerdictDeferred, "defer"},
}

func ParseVerdictVerb(verb string) (Verdict, bool) {
	for _, entry := range verdictVerbs {
		if entry.verb == verb {
			return entry.verdict, true
		}
	}
	return "", false
}

const MaxVerdictReasonBytes = 240

// VerdictReason is a trimmed, single-line reason of 1 to MaxVerdictReasonBytes bytes.
type VerdictReason struct{ text string }

func ParseVerdictReason(text string) (VerdictReason, error) {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return VerdictReason{}, fmt.Errorf("the reason is empty")
	case strings.ContainsAny(trimmed, "\r\n"):
		return VerdictReason{}, fmt.Errorf("the reason spans more than one line")
	case len(trimmed) > MaxVerdictReasonBytes:
		return VerdictReason{}, fmt.Errorf("the reason is %d bytes; shorten it to %d", len(trimmed), MaxVerdictReasonBytes)
	}
	return VerdictReason{text: trimmed}, nil
}

func (reason VerdictReason) String() string { return reason.text }

// Judgment is one Verdict a Caller records against a Finding ordinal.
type Judgment struct {
	Ordinal int
	Verdict Verdict
	Reason  VerdictReason
}

// FindingVerdict is the latest Verdict on one Finding. It is Stale when the
// Finding's text no longer matches the text the Verdict judged.
type FindingVerdict struct {
	ReviewID   ReviewID  `json:"review_id"`
	Repository string    `json:"repository"`
	Profile    string    `json:"profile"`
	Ordinal    int       `json:"ordinal"`
	Location   string    `json:"location,omitempty"`
	Verdict    Verdict   `json:"verdict"`
	Reason     string    `json:"reason"`
	RecordedBy string    `json:"recorded_by"`
	RecordedAt time.Time `json:"recorded_at"`
	Stale      bool      `json:"stale,omitempty"`
}

// FindingDigest identifies a Finding's full text, so a Verdict never carries
// over to different text recorded later under the same ordinal.
func FindingDigest(finding Finding) string {
	var text []byte
	for _, field := range []string{finding.Severity, finding.Category, finding.Location, finding.Failure, finding.Evidence, finding.Fix, finding.Test} {
		text = fmt.Appendf(text, "%d:%s", len(field), field)
	}
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:8])
}
