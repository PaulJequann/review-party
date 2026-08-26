package main

import (
	"fmt"
	"io"
	"os"

	"reviewparty/internal/configuration"
)

// defaultUserConfigurationPath returns the canonical Global Configuration
// file, or an empty path when the platform provides no configuration home.
func defaultUserConfigurationPath() string {
	root := configuration.DefaultGlobalRoot()
	if root == "" {
		return ""
	}
	return root + string(os.PathSeparator) + "config.json"
}

func printConfigurationPath(path string, output io.Writer) error {
	_, err := fmt.Fprintln(output, path)
	return err
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
