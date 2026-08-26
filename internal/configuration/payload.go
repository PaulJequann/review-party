package configuration

import "fmt"

type payloadConstraint struct {
	description string
	maximum     int
}

var (
	profileMetadataPayload     = payloadConstraint{description: "Profile metadata", maximum: MaximumDocumentBytes}
	profileInstructionsPayload = payloadConstraint{description: "Profile instructions", maximum: MaximumDocumentBytes}
	partyDefinitionPayload     = payloadConstraint{description: "Party", maximum: maximumPartyBytes}
)

func (constraint payloadConstraint) validate(payload []byte) error {
	if len(payload) > constraint.maximum {
		return fmt.Errorf("%s exceeds %d bytes", constraint.description, constraint.maximum)
	}
	return nil
}
