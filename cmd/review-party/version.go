package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"reviewparty/internal/model"
	"reviewparty/internal/provenance"
)

var currentRuntimeProvenance = provenance.CurrentRuntimeProvenance

func newVersionCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build provenance for this Review Party binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeVersion(streams.output, stringFlag(cmd, "format"), currentRuntimeProvenance())
		},
	}
	addFormatFlag(cmd)
	return cmd
}

func writeVersion(output io.Writer, format string, runtime model.RuntimeProvenance) error {
	switch format {
	case "human":
		_, err := io.WriteString(output, humanVersion(runtime))
		return err
	case "json":
		return json.NewEncoder(output).Encode(runtime)
	default:
		return fmt.Errorf("unsupported format %q; expected human or json", format)
	}
}

func humanVersion(runtime model.RuntimeProvenance) string {
	version := runtime.Version
	if version == "" {
		version = "development"
	}
	text := fmt.Sprintf("Review Party %s\n", version)
	if runtime.VCSRevision != "" {
		text += fmt.Sprintf("revision: %s\n", runtime.VCSRevision)
	}
	if runtime.VCSModified != nil {
		text += fmt.Sprintf("modified: %t\n", *runtime.VCSModified)
	}
	return text
}
