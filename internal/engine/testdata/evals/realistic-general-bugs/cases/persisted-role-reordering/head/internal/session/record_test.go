package session

import (
	"testing"

	"example.com/reporting/internal/auth"
)

func TestCurrentViewerRoundTrip(t *testing.T) {
	payload, err := Encode(Record{Subject: "user-1", Role: auth.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	if record.Role != auth.RoleViewer {
		t.Fatalf("role = %v", record.Role)
	}
}
