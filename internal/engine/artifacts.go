package engine

import "reviewparty/internal/artifact"

import (
	"reviewparty/internal/model"
)

// artifactPublisher owns artifact I/O: truncation, publishing, removal, and
// verification. It keeps artifact concerns local so lifecycle orchestration
// in review.go stays focused on pending → availability → execution →
// validation → persistence. Changing truncation or publishing touches only
// this file.
type artifactPublisher struct {
	store *artifact.Store
}

func newArtifactPublisher(store *artifact.Store) *artifactPublisher {
	return &artifactPublisher{store: store}
}

func (publisher *artifactPublisher) publishAttemptArtifacts(id model.ReviewID, number int, prompt string, execution attemptExecution) ([]model.ArtifactReference, error) {
	inputs := []struct {
		kind      string
		contents  []byte
		truncated bool
	}{
		{kind: "constructed-prompt", contents: []byte(prompt), truncated: len(prompt) > maxHarnessStdout},
		{kind: "assistant-text", contents: []byte(execution.AssistantText), truncated: execution.ArtifactTruncated || len(execution.AssistantText) > maxHarnessStdout},
	}
	references := make([]model.ArtifactReference, 0, len(inputs))
	for _, input := range inputs {
		contents := boundedArtifactContents(input.contents)
		reference, err := publisher.store.Publish(id, number, input.kind, contents, input.truncated)
		if err != nil {
			publisher.removeArtifacts(references)
			return nil, err
		}
		references = append(references, reference)
	}
	return references, nil
}

func (publisher *artifactPublisher) removeArtifacts(references []model.ArtifactReference) {
	if publisher.store == nil {
		return
	}
	for _, reference := range references {
		_ = publisher.store.Remove(reference)
	}
}

func (publisher *artifactPublisher) verifyArtifacts(record model.ReviewRecord) error {
	if publisher.store == nil {
		return nil
	}
	for _, pass := range record.Passes {
		for _, attempt := range pass.Attempts {
			for _, reference := range attempt.Artifacts {
				if _, err := publisher.store.Read(reference); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func boundedArtifactContents(contents []byte) []byte {
	if len(contents) <= maxHarnessStdout {
		return contents
	}
	return contents[:maxHarnessStdout]
}
