package profile

import (
	"context"

	"example.com/console/internal/cache"
	"example.com/console/internal/identity"
)

type Profile struct {
	DisplayName string
	Email       string
}

type Cache interface {
	Get(context.Context, string) (Profile, bool)
	Put(context.Context, string, Profile)
}

type Loader interface {
	Load(context.Context, identity.Identity) (Profile, error)
}

type Service struct {
	cache  Cache
	loader Loader
}

func NewService(cacheStore Cache, loader Loader) Service {
	return Service{cache: cacheStore, loader: loader}
}

func (service Service) Get(ctx context.Context, subject identity.Identity) (Profile, error) {
	key := cache.UserProfileKey(subject.TenantID, subject.UserID)
	if cached, found := service.cache.Get(ctx, key); found {
		return cached, nil
	}
	loaded, err := service.loader.Load(ctx, subject)
	if err != nil {
		return Profile{}, err
	}
	service.cache.Put(ctx, key, loaded)
	return loaded, nil
}
