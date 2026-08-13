package httpapi

import "example.com/reporting/internal/session"

func MayExport(record session.Record) bool {
	return record.Role.CanExport()
}
