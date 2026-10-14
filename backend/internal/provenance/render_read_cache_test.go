package provenance

import (
	"context"
	"io"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

type signingCacheObjects struct {
	calls int
	body  string
}

func (s *signingCacheObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	s.calls++
	return io.NopCloser(strings.NewReader(s.body)), nil
}
func TestSigningRenderPhysicalReadCacheIsInvocationAndReaderBound(t *testing.T) {
	raw := "array"
	object := objectstore.Object{Key: "array", VersionID: "v1", ContentType: "application/x-npy", Bytes: int64(len(raw)), SHA256: bodyDigest([]byte(raw))}
	reader := &signingCacheObjects{body: raw}
	ctx := newSigningRenderReadContext(context.Background())
	for i := 0; i < 2; i++ {
		if err := hashSigningRenderObject(ctx, reader, object, 100); err != nil {
			t.Fatal(err)
		}
	}
	if reader.calls != 1 {
		t.Fatal("verified bytes reread", reader.calls)
	}
	if err := hashSigningRenderObject(ctx, reader, object, 1); err == nil {
		t.Fatal("cache bypassed byte budget")
	}
	other := &signingCacheObjects{body: raw}
	if err := hashSigningRenderObject(ctx, other, object, 100); err != nil || other.calls != 1 {
		t.Fatal("cache crossed object readers", err)
	}
	object.VersionID = "v2"
	if err := hashSigningRenderObject(ctx, reader, object, 100); err != nil || reader.calls != 2 {
		t.Fatal("cache ignored object version", err)
	}
	if err := hashSigningRenderObject(newSigningRenderReadContext(ctx), reader, object, 100); err != nil || reader.calls != 3 {
		t.Fatal("cache crossed invocations", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := hashSigningRenderObject(cancelled, reader, object, 100); err != context.Canceled {
		t.Fatal("cache ignored cancellation", err)
	}
}
func TestSigningRenderReadCacheNeverRetainsFailedHash(t *testing.T) {
	object := objectstore.Object{Key: "array", VersionID: "v1", Bytes: 5, SHA256: bodyDigest([]byte("array"))}
	reader := &signingCacheObjects{body: "wrong"}
	ctx := newSigningRenderReadContext(context.Background())
	if err := hashSigningRenderObject(ctx, reader, object, 100); err == nil {
		t.Fatal("bad bytes verified")
	}
	reader.body = "array"
	if err := hashSigningRenderObject(ctx, reader, object, 100); err != nil || reader.calls != 2 {
		t.Fatal("failed hash cached", err)
	}
}

func TestSigningRenderReadCacheRemainsBounded(t *testing.T) {
	ctx := newSigningRenderReadContext(context.Background())
	reader := &signingCacheObjects{body: "x"}
	cache := ctx.Value(signingRenderReadCacheContextKey{}).(*signingRenderReadCache)
	for i := 0; i < 4100; i++ {
		object := objectstore.Object{Key: strings.Repeat("x", i+1), VersionID: "v1", Bytes: 1, SHA256: bodyDigest([]byte("x"))}
		if err := hashSigningRenderObject(ctx, reader, object, 1); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.verified) != 4096 {
		t.Fatal("read cache exceeded bound", len(cache.verified))
	}
}
