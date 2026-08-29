package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func scanJSONLines[T any](output []byte, adapter string, apply func(T)) error {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event T
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode %s event: %w", adapter, err)
		}
		apply(event)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s output: %w", adapter, err)
	}
	return nil
}
