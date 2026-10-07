package engine

import (
	"errors"

	"reviewparty/internal/artifact"
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

type artifactInput struct {
	kind      string
	contents  []byte
	truncated bool
}

func (publisher *artifactPublisher) publishAttemptArtifacts(id model.ReviewID, number int, execution attemptExecution) ([]model.ArtifactReference, error) {
	inputs := []artifactInput{
		{kind: artifact.AssistantText, contents: []byte(execution.AssistantText), truncated: execution.ArtifactTruncated || len(execution.AssistantText) > maxHarnessStdout},
	}
	if execution.ReviewerNoise != "" {
		inputs = append(inputs, artifactInput{kind: artifact.ReviewerNoise, contents: []byte(execution.ReviewerNoise), truncated: len(execution.ReviewerNoise) > maxHarnessStdout})
	}
	references := make([]model.ArtifactReference, 0, len(inputs))
	for _, input := range inputs {
		contents := boundedArtifactContents(input.contents)
		reference, err := publisher.store.Publish(id, number, input.kind, contents, input.truncated)
		if err != nil {
			cleanupErr := publisher.removeArtifacts(references)
			return nil, errors.Join(err, cleanupErr)
		}
		references = append(references, reference)
	}
	return references, nil
}

func (publisher *artifactPublisher) removeArtifacts(references []model.ArtifactReference) error {
	if publisher.store == nil {
		return nil
	}
	var cleanupErr error
	for _, reference := range references {
		cleanupErr = errors.Join(cleanupErr, publisher.store.Remove(reference))
	}
	return cleanupErr
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
