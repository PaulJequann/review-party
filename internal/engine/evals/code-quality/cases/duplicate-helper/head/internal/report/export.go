package report

import "time"

func ExportTimestamp(value string) (string, error) {
	timestamp, err := parseExportDate(value)
	if err != nil {
		return "", err
	}
	return timestamp.UTC().Format(time.RFC3339), nil
}

func parseExportDate(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}
