package importer

type Record struct{ Value string }

func Import(raw string, save func(Record) error) error {
	record := normalize(raw)
	if err := validate(record); err != nil {
		return err
	}
	return save(record)
}
