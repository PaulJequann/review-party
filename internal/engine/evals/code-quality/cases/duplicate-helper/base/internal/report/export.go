package report

import (
	"time"

	"example.com/quality/reports/internal/dates"
)

func ExportTimestamp(value string) (string, error) {
	timestamp, err := dates.Parse(value)
	if err != nil {
		return "", err
	}
	return timestamp.UTC().Format(time.RFC3339), nil
}
