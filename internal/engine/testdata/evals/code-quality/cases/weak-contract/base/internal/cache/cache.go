package cache

type Key struct{ UserID string }
type Entry struct{ DisplayName string }

type Cache interface {
	Lookup(Key) (Entry, bool)
}
