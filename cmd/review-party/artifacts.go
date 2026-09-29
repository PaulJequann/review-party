package main

import (
	"reviewparty/internal/model"
)

func writeArtifactReferences(output *commandOutput, record model.ReviewRecord) {
	for _, pass := range record.Passes {
		for _, attempt := range pass.Attempts {
			for _, artifact := range attempt.Artifacts {
				writeArtifactReference(output, artifact)
			}
		}
	}
}

func writeArtifactReference(output *commandOutput, artifact model.ArtifactReference) {
	output.write("artifact: %s · %s · %d bytes · sha256:%s", artifact.Kind, artifact.Path, artifact.Size, artifact.Digest)
	if artifact.Truncated {
		output.write(" · truncated")
	}
	output.write("\n")
}
