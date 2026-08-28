package cache

type Entry struct{ DisplayName string }

type Cache interface {
	Lookup(any) (any, bool)
}
