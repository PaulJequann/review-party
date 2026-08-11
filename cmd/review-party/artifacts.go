package main

import (
	"fmt"
	"io"
	"reviewparty/internal/model"
)

func printArtifactReferences(output io.Writer, record model.ReviewRecord) {
	for _, pass := range record.Passes {
		for _, attempt := range pass.Attempts {
			for _, artifact := range attempt.Artifacts {
				fmt.Fprintf(output, "artifact: %s · %s · %d bytes · sha256:%s", artifact.Kind, artifact.Path, artifact.Size, artifact.Digest)
				if artifact.Truncated {
					fmt.Fprint(output, " · truncated")
				}
				fmt.Fprintln(output)
			}
		}
	}
}
