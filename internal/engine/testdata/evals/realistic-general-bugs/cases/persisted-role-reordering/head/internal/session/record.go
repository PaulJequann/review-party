package session

import (
	"encoding/json"

	"example.com/reporting/internal/auth"
)

type Record struct {
	Subject string    `json:"subject"`
	Role    auth.Role `json:"role"`
}

func Encode(record Record) ([]byte, error) {
	return json.Marshal(record)
}

func Decode(payload []byte) (Record, error) {
	var record Record
	err := json.Unmarshal(payload, &record)
	return record, err
}
