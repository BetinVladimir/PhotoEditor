package v2optical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type perspectiveLinesTestCatalog struct {
	locator PerspectiveLinesLocator
	calls   int
	change  bool
}

func (c *perspectiveLinesTestCatalog) ResolvePerspectiveLines(_ context.Context, scope PerspectiveLinesScope) (PerspectiveLinesLocator, error) {
	c.calls++
	got := c.locator
	got.Scope = scope
	if c.change && c.calls > 1 {
		got.Version = "replaced"
	}
	return got, nil
}

type perspectiveLinesTestReader struct {
	raw    []byte
	calls  int
	closed bool
}
type perspectiveLinesTestBody struct {
	*bytes.Reader
	owner *perspectiveLinesTestReader
}

func (b perspectiveLinesTestBody) Close() error { b.owner.closed = true; return nil }
func (r *perspectiveLinesTestReader) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	r.calls++
	return perspectiveLinesTestBody{Reader: bytes.NewReader(r.raw), owner: r}, nil
}
func TestPerspectiveLinesLoaderOriginalAndVersionReplay(t *testing.T) {
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"},
		{A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"},
		{A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"}}}
	raw, _, err := lines.Compile()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	p := perspectiveIntent()
	p.Mode = "auto"
	p.Corners = nil
	ref := p.Base.Source
	ref.ArtifactID = uuid.New()
	ref.SHA256 = hex.EncodeToString(digest[:])
	p.Lines = &ref
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, change := range []bool{false, true} {
		catalog := &perspectiveLinesTestCatalog{locator: PerspectiveLinesLocator{Bucket: "photos", Key: "lines", Version: "v1", Media: "application/json", Bytes: int64(len(raw))}, change: change}
		objects := &perspectiveLinesTestReader{raw: raw}
		got, err := (PerspectiveLinesLoader{Catalog: catalog, Objects: objects, Bucket: "photos"}).Load(context.Background(), who, uuid.New(), uuid.New(), uuid.New(), p)
		if change {
			if err == nil {
				t.Fatal("replaced version accepted")
			}
		} else if err != nil || len(got.Lines) != 4 {
			t.Fatal(got, err)
		}
		if catalog.calls != 2 || objects.calls != 1 || !objects.closed {
			t.Fatal(catalog.calls, objects)
		}
	}
}
func TestPerspectiveLinesLoaderRefusesInvalidBeforeIO(t *testing.T) {
	catalog := &perspectiveLinesTestCatalog{}
	objects := &perspectiveLinesTestReader{}
	loader := PerspectiveLinesLoader{Catalog: catalog, Objects: objects, Bucket: "photos"}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	p := perspectiveIntent()
	if _, err := loader.Load(context.Background(), who, uuid.New(), uuid.New(), uuid.New(), p); err == nil {
		t.Fatal("manual accepted")
	}
	p.Mode = "auto"
	p.Corners = nil
	p.Lines = &p.Base.Source
	if _, err := loader.Load(context.Background(), who, uuid.New(), uuid.New(), uuid.New(), p); err == nil {
		t.Fatal("source alias accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Load(ctx, who, uuid.New(), uuid.New(), uuid.New(), p); err != context.Canceled {
		t.Fatal(err)
	}
	if catalog.calls != 0 || objects.calls != 0 {
		t.Fatal("invalid request performed IO")
	}
}
