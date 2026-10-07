package main

import (
	"reviewparty/internal/model"
)

func writeArtifactReferences(output *commandOutput, record model.ReviewRecord) {
	for _, artifact := range record.Artifacts() {
		writeArtifactReference(output, artifact)
	}
}

func writeArtifactReference(output *commandOutput, artifact model.ArtifactReference) {
	output.write("artifact: %s · %s · %d bytes · sha256:%s", artifact.Kind, artifact.Path, artifact.Size, artifact.Digest)
	if artifact.Truncated {
		output.write(" · truncated")
	}
	output.write("\n")
}
