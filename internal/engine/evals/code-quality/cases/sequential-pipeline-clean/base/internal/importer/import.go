package importer

import "errors"

var ErrInvalid = errors.New("invalid record")

type Record struct{ Value string }

func Import(raw string, save func(Record) error) error {
	record := Record{Value: raw}
	if record.Value == "" {
		return ErrInvalid
	}
	return save(record)
}
