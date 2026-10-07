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

// publishAttemptArtifacts keeps the raw output of a failed attempt. Empty
// streams leave no file.
func (publisher *artifactPublisher) publishAttemptArtifacts(id model.ReviewID, number int, execution attemptExecution) ([]model.ArtifactReference, error) {
	if publisher == nil || publisher.store == nil {
		return nil, nil
	}
	streams := []artifact.Evidence{
		{Kind: artifact.AssistantText, Contents: []byte(execution.AssistantText), Truncated: execution.ArtifactTruncated || len(execution.AssistantText) > maxHarnessStdout},
		{Kind: artifact.ReviewerNoise, Contents: []byte(execution.ReviewerNoise), Truncated: len(execution.ReviewerNoise) > maxHarnessStdout},
	}
	var references []model.ArtifactReference
	for _, evidence := range streams {
		if len(evidence.Contents) == 0 {
			continue
		}
		evidence.Contents = boundedArtifactContents(evidence.Contents)
		reference, err := publisher.store.Publish(id, number, evidence)
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
	for _, reference := range record.Artifacts() {
		if _, err := publisher.store.Read(reference); err != nil {
			return err
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
