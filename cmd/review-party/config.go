package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"reviewparty/internal/configuration"
)

// defaultUserConfigurationPath returns the canonical Personal Configuration
// file, or an empty path when the platform provides no configuration home.
func defaultUserConfigurationPath() string {
	root := configuration.DefaultPersonalRoot()
	if root == "" {
		return ""
	}
	return root + string(os.PathSeparator) + "config.json"
}

func runConfig(arguments []string, stdout, stderr io.Writer) int {
	operation, configuration, exitCode := parseConfigOptions(arguments, stderr)
	if exitCode != 0 {
		return exitCode
	}
	switch operation {
	case "path":
		fmt.Fprintln(stdout, configuration)
		return 0
	case "show":
		if err := showConfiguration(configuration, stdout); err != nil {
			return printFailure(stderr, err)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "review-party: unknown config operation %q\n", operation)
		return 2
	}
}

func parseConfigOptions(arguments []string, stderr io.Writer) (string, string, int) {
	if len(arguments) == 0 {
		fmt.Fprintln(stderr, "review-party: config requires path or show")
		return "", "", 2
	}
	operation := arguments[0]
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(arguments[1:]); err != nil {
		return "", "", 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: config accepts one operation")
		return "", "", 2
	}
	return operation, *configuration, 0
}

func showConfiguration(path string, output io.Writer) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read user configuration %q: %w", path, err)
	}
	if _, err := output.Write(payload); err != nil {
		return err
	}
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		fmt.Fprintln(output)
	}
	return nil
}
