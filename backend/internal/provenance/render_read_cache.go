package provenance

import (
	"context"
	"photoeditor/backend/internal/objectstore"
	"reflect"
	"sync"
)

type signingRenderReadCacheContextKey struct{}
type signingRenderReadCacheKey struct {
	Reader SigningObjects
	Object objectstore.Object
}
type signingRenderReadCache struct {
	mu       sync.Mutex
	verified map[signingRenderReadCacheKey]bool
}

// A fresh private cache is created at each resolver invocation. It stores only
// successful physical hashes, not ACL/producer/review decisions or image bytes.
func newSigningRenderReadContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, signingRenderReadCacheContextKey{}, &signingRenderReadCache{verified: map[signingRenderReadCacheKey]bool{}})
}
func signingRenderCachedObject(ctx context.Context, reader SigningObjects, object objectstore.Object) (*signingRenderReadCache, signingRenderReadCacheKey, bool) {
	cache, _ := ctx.Value(signingRenderReadCacheContextKey{}).(*signingRenderReadCache)
	if cache == nil || reader == nil || !reflect.ValueOf(reader).Comparable() {
		return nil, signingRenderReadCacheKey{}, false
	}
	key := signingRenderReadCacheKey{reader, object}
	cache.mu.Lock()
	hit := cache.verified[key]
	cache.mu.Unlock()
	return cache, key, hit
}
func (c *signingRenderReadCache) remember(key signingRenderReadCacheKey) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.verified) < 4096 {
		c.verified[key] = true
	}
}
