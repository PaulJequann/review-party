package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

type configurationMutationOptions struct {
	repository    string
	format        string
	configuration string
	yes           bool
	scope         configuration.Scope
}

type configurationFileOptions struct {
	scope         string
	repository    string
	format        string
	configuration string
}

type valueReport[T any] struct {
	Value    T      `json:"value"`
	Authored bool   `json:"authored"`
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
}

type configurationFileStatus struct {
	Scope   string `json:"scope"`
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

type configurationPlanResult struct {
	Valid     bool                  `json:"valid"`
	Published bool                  `json:"published"`
	Reason    string                `json:"reason,omitempty"`
	Warnings  []string              `json:"warnings,omitempty"`
	Scopes    []string              `json:"scopes,omitempty"`
	Paths     []string              `json:"paths,omitempty"`
	Changes   []configurationChange `json:"changes,omitempty"`
}

type configurationChange struct {
	Field     string `json:"field"`
	Scope     string `json:"scope"`
	Path      string `json:"path"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	HadBefore bool   `json:"had_before"`
	HadAfter  bool   `json:"had_after"`
}

func parseConfigurationScope(value string) (configuration.Scope, error) {
	scope := configuration.Scope(value)
	if scope != configuration.ScopeGlobal && scope != configuration.ScopeRepository {
		return "", fmt.Errorf("unknown configuration scope %q; expected global or repository", value)
	}
	return scope, nil
}

func validateConfigurationFormat(format string) error {
	if format != "human" && format != "json" {
		return fmt.Errorf("unknown output format %q", format)
	}
	return nil
}

func addConfigMutationFlags(cmd *cobra.Command, withScope, withSelection bool, scopeDefault string) {
	cmd.Flags().Bool("yes", false, "Publish without an interactive confirmation")
	addRepositoryFlag(cmd, "Repository for repository-scoped configuration")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	if withScope {
		cmd.Flags().String("scope", scopeDefault, "Configuration scope: global or repository")
	}
	if withSelection {
		cmd.Flags().String("profile", "", "Profile name to select")
		cmd.Flags().String("party", "", "Party name to select")
	}
}

func runConfigurationCommand(format, configurationPath string, streams commandIO, action func(*configuration.Manager) (int, error)) int {
	if err := validateConfigurationFormat(format); err != nil {
		return printConfigFailure(format, streams.output, streams.errors, err)
	}
	if streams.configurationManager == nil {
		return printConfigFailure(format, streams.output, streams.errors, fmt.Errorf("configuration manager factory is unavailable"))
	}
	code, err := action(streams.configurationManager(configurationPath))
	if err != nil {
		return printConfigFailure(format, streams.output, streams.errors, err)
	}
	return code
}

func printConfigFailure(format string, stdout, stderr io.Writer, err error) int {
	if format == "json" {
		if writeErr := writeJSON(stdout, map[string]string{"error": err.Error()}); writeErr == nil {
			return 1
		}
	}
	return printFailure(stderr, err)
}

func printConfigOutput(streams commandIO, render func(*commandOutput)) int {
	if err := writeCommandOutput(streams.output, render); err != nil {
		return printConfigFailure("human", streams.output, streams.errors, err)
	}
	return 0
}

func instructionFlags(cmd *cobra.Command) (string, error) {
	instructions := stringFlag(cmd, "instructions")
	path := stringFlag(cmd, "instructions-file")
	if path == "" {
		return instructions, nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read instructions file %q: %w", path, err)
	}
	return string(payload), nil
}

func valueReportOf[T any](value configuration.Value[T]) valueReport[T] {
	return valueReport[T]{Value: value.Value, Authored: value.Authored, Source: string(value.Source), Path: value.Path}
}

func printHumanValue[T any](output *commandOutput, label string, value valueReport[T]) {
	if value.Authored {
		output.write("%s: %v (%s %s)\n", label, value.Value, value.Source, value.Path)
		return
	}
	output.write("%s: %v (%s)\n", label, value.Value, value.Source)
}
