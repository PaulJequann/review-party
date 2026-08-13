package store

type Store interface {
	Put(key, value string) error
}
