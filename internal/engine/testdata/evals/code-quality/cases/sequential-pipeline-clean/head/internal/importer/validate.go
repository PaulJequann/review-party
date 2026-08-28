package importer

import "errors"

var ErrInvalid = errors.New("invalid record")

func validate(record Record) error {
	if record.Value == "" {
		return ErrInvalid
	}
	return nil
}
