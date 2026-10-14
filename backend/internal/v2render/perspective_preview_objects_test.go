package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type previewObjectFixture struct {
	object  objectstore.Object
	calls   int
	change  bool
	revoked bool
}

func (f *previewObjectFixture) ResolveObject(_ context.Context, _ PerspectivePreviewSource, _ v2draft.LayerRef) (objectstore.Object, error) {
	f.calls++
	if f.revoked && f.calls > 1 {
		return objectstore.Object{}, v2input.ErrExpired
	}
	obj := f.object
	if f.change && f.calls > 1 {
		obj.VersionID = "replaced"
	}
	return obj, nil
}
func TestPerspectivePreviewDocumentOriginalBytesAndCurrentLocator(t *testing.T) {
	for _, mode := range []string{"valid", "version", "revoked", "sha", "truncated", "noncanonical"} {
		t.Run(mode, func(t *testing.T) {
			pin := perspectivePreviewSourceFixture(t)
			raw := []byte(`{"schema_version":1}`)
			if mode == "noncanonical" {
				raw = []byte(`{ "schema_version":1}`)
			}
			sum := sha256.Sum256(raw)
			pin.Result.SHA256 = hex.EncodeToString(sum[:])
			obj := objectstore.Object{Key: "root", VersionID: "immutable", SHA256: pin.Result.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
			fixture := &previewObjectFixture{object: obj, change: mode == "version", revoked: mode == "revoked"}
			objects := &adoptedRootFixture{body: raw}
			if mode == "sha" {
				objects.body = []byte(`{"schema_version":2}`)
			}
			if mode == "truncated" {
				objects.body = raw[:len(raw)-1]
			}
			body, err := ReadPerspectivePreviewDocument(context.Background(), fixture, objects, pin, v2draft.LayerRef{Ref: pin.Result, Contract: "ImageResult", Path: "$"})
			if mode == "valid" {
				if err != nil || string(body) != string(raw) || fixture.calls != 2 {
					t.Fatal("valid read failed", err)
				}
			} else if err == nil {
				t.Fatal("invalid original accepted", mode)
			}
			if objects.opens != 1 || objects.closes != 1 {
				t.Fatal("stream leak", objects.opens, objects.closes)
			}
		})
	}
}

func TestPerspectivePreviewNodeRejectsAliasesAndOpaqueTransforms(t *testing.T) {
	pin := perspectivePreviewSourceFixture(t)
	for _, node := range []v2draft.LayerRef{
		{Ref: pin.Result, Contract: "WorkingImage", Path: "$"},
		{Ref: pin.Result, Contract: "ImageResult", Path: "$/working_image"},
		{Ref: pin.Base.Source, Contract: "WorkingImage", Path: "$/working_image"},
		{Ref: pin.Base.Source, Contract: "Artifact", Path: "$/report/identity"},
	} {
		if _, _, err := perspectivePreviewNodeLimit(pin, node); err == nil {
			t.Fatal("invalid occurrence accepted", node)
		}
	}
	if _, limit, err := perspectivePreviewNodeLimit(pin, v2draft.LayerRef{Ref: pin.Base.Source, Contract: "WorkingImage", Path: "$/source"}); err != nil || limit != 65536 {
		t.Fatal("source limit", limit, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadPerspectivePreviewDocument(ctx, nil, nil, pin, v2draft.LayerRef{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
