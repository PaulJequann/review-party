package engine

import (
	"fmt"
	"strings"
)

func (library profileLibrary) missingProfileError(repository, name string, searched []string) error {
	formatted := make([]string, 0, len(searched))
	for _, source := range searched {
		formatted = append(formatted, "  "+source)
	}
	return fmt.Errorf("profile %q was not found\nsearched:\n%s\navailable: %s", name, strings.Join(formatted, "\n"), listOrNone(library.executableProfileNames(repository)))
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}
