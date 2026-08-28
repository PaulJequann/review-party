package profile

import "example.com/quality/profile/internal/cache"

func Name(store cache.Cache, userID string) (string, bool) {
	entry, ok := store.Lookup(cache.Key{UserID: userID})
	return entry.DisplayName, ok
}
