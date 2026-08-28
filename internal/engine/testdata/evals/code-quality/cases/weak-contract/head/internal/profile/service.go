package profile

import "example.com/quality/profile/internal/cache"

func Name(store cache.Cache, userID string) (string, bool) {
	value, ok := store.Lookup(userID)
	if !ok {
		return "", false
	}
	return value.(cache.Entry).DisplayName, true
}
