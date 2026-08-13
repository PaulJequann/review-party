package dates

import "time"

func Parse(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}
