package store

type Store interface {
	Put(key, value string) error
}

type AuditLog interface {
	Record(key string) error
}

type AuditedStore struct {
	Inner Store
	Audit AuditLog
}

func (store AuditedStore) Put(key, value string) error {
	if err := store.Inner.Put(key, value); err != nil {
		return err
	}
	return store.Audit.Record(key)
}
