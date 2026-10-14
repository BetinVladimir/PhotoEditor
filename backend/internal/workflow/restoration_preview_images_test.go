package workflow

import (
	"bytes"
	"context"
	"io"
	"testing"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type restorationImageCatalogFixture struct {
	object objectstore.Object
	calls  int
	drift  bool
}

func (f *restorationImageCatalogFixture) Resolve(_ context.Context, _ RestorationPreviewSource, _ string) (objectstore.Object, error) {
	f.calls++
	obj := f.object
	if f.drift && f.calls > 1 {
		obj.VersionID = "replaced"
	}
	return obj, nil
}

type restorationImageObjectFixture struct {
	raw   []byte
	calls int
}

func (f *restorationImageObjectFixture) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	f.calls++
	return io.NopCloser(bytes.NewReader(f.raw)), nil
}

func TestRestorationPreviewImageRejectsChangedPinAndBytes(t *testing.T) {
	_, pin, raw, _ := restorationPreviewFixture(t)
	obj := objectstore.Object{Key: "tenant/source", VersionID: "v1",
		SHA256: pin.Base.Source.SHA256, ContentType: "application/json", Bytes: int64(len(raw))}
	catalog := &restorationImageCatalogFixture{object: obj}
	objects := &restorationImageObjectFixture{raw: raw}
	got, err := readRestorationPreviewImage(context.Background(), catalog, objects, pin, "before")
	if err != nil || !bytes.Equal(got, raw) || catalog.calls != 2 || objects.calls != 1 {
		t.Fatal("valid pinned image refused", err)
	}
	catalog.calls, catalog.drift = 0, true
	if _, err = readRestorationPreviewImage(context.Background(), catalog, objects, pin, "before"); err != v2input.ErrExpired {
		t.Fatal("changed object pin accepted", err)
	}
	catalog.calls, catalog.drift = 0, false
	objects.raw = append([]byte(nil), raw...)
	objects.raw[len(objects.raw)-2] ^= 1
	if _, err = readRestorationPreviewImage(context.Background(), catalog, objects, pin, "before"); err == nil {
		t.Fatal("changed JSON bytes accepted")
	}
	if _, err = readRestorationPreviewImage(context.Background(), catalog, objects, pin, "other"); err == nil {
		t.Fatal("unregistered side accepted")
	}
}
