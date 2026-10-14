package v2optical

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiSourceArraySpecsKeepFrameScope(t *testing.T) {
	p, originals := multiOriginals(t)
	raw, _, _ := p.Compile()
	specs, err := MultiSourceArrays("optical.panorama", raw, originals)
	if err != nil || len(specs) != 2 {
		t.Fatalf("%v specs=%d", err, len(specs))
	}
	for i, spec := range specs {
		if spec.Ordinal != i || spec.Binding != p.Frames[i] || spec.Array.Role != "WorkingPixels" || spec.Array.Channels != 3 || spec.Array.Width != 100 || spec.Array.Height != 100 {
			t.Fatal("lost frame scope")
		}
	}
}

type refusingMultiArrayCatalog struct{ calls int }

func (c *refusingMultiArrayCatalog) ResolveMultiSourceArray(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string, []byte, []byte, MultiSourceArraySpec) (objectstore.Object, error) {
	c.calls++
	return objectstore.Object{}, v2input.ErrExpired
}
func TestMultiArrayCancellationPrecedesAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	catalog := &refusingMultiArrayCatalog{}
	err := VerifyMultiSourceObjects(ctx, catalog, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, "optical.panorama", nil, nil)
	if !errors.Is(err, context.Canceled) || catalog.calls != 0 {
		t.Fatalf("%v calls=%d", err, catalog.calls)
	}
}
