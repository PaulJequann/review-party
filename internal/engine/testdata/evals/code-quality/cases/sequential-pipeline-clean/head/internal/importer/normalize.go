package importer

import "strings"

func normalize(raw string) Record {
	return Record{Value: strings.TrimSpace(raw)}
}
