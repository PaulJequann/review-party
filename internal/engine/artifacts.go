package engine

import "time"

func (conductor *Conductor) buildAttempt(id ReviewID, prompt string, candidate reviewerCandidate, execution attemptExecution, outcome AttemptOutcome, started, completed time.Time) (AttemptRecord, error) {
	attempt := AttemptRecord{
		Number:      1,
		Outcome:     outcome,
		Provenance:  resolvedProvenance(candidate, execution),
		Diagnostic:  execution.Diagnostic,
		RawOutput:   boundedAttemptOutput(execution.AssistantText),
		StartedAt:   started,
		CompletedAt: completed,
	}
	if conductor.artifacts == nil {
		return attempt, nil
	}
	references, err := conductor.publishAttemptArtifacts(id, attempt.Number, prompt, execution)
	if err != nil {
		return AttemptRecord{}, err
	}
	attempt.Artifacts = references
	attempt.RawOutput = ""
	return attempt, nil
}

func (conductor *Conductor) publishAttemptArtifacts(id ReviewID, number int, prompt string, execution attemptExecution) ([]ArtifactReference, error) {
	inputs := []struct {
		kind      string
		contents  []byte
		truncated bool
	}{
		{kind: "constructed-prompt", contents: []byte(prompt), truncated: len(prompt) > maxHarnessStdout},
		{kind: "assistant-text", contents: []byte(execution.AssistantText), truncated: execution.ArtifactTruncated || len(execution.AssistantText) > maxHarnessStdout},
	}
	references := make([]ArtifactReference, 0, len(inputs))
	for _, input := range inputs {
		contents := boundedArtifactContents(input.contents)
		reference, err := conductor.artifacts.Publish(id, number, input.kind, contents, input.truncated)
		if err != nil {
			conductor.removeArtifacts(references)
			return nil, err
		}
		references = append(references, reference)
	}
	return references, nil
}

func boundedArtifactContents(contents []byte) []byte {
	if len(contents) <= maxHarnessStdout {
		return contents
	}
	return contents[:maxHarnessStdout]
}

func (conductor *Conductor) removeArtifacts(references []ArtifactReference) {
	for _, reference := range references {
		_ = conductor.artifacts.Remove(reference)
	}
}

func (conductor *Conductor) VerifyArtifacts(record ReviewRecord) error {
	if conductor.artifacts == nil {
		return nil
	}
	for _, pass := range record.Passes {
		for _, attempt := range pass.Attempts {
			for _, reference := range attempt.Artifacts {
				if _, err := conductor.artifacts.Read(reference); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
