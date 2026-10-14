package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type restorationDocumentCatalogFixture struct {
	objects map[string]objectstore.Object
	reads   map[string]int
	change  bool
}

func (f *restorationDocumentCatalogFixture) ResolveRestorationDocument(_ context.Context,
	_ RestorationArrayScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	object, ok := f.objects[role]
	if !ok || object.SHA256 != ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	f.reads[role]++
	if f.change && role == "image" && f.reads[role] > 1 {
		object.VersionID = "new-version"
	}
	return object, nil
}

type restorationDocumentObjectsFixture map[string][]byte

func (f restorationDocumentObjectsFixture) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f[object.Key])), nil
}

func TestRestorationResultDocumentLoaderPinsRootChildrenAndVersions(t *testing.T) {
	marshal := func(value any) []byte {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	source := marshal(map[string]any{"source": true})
	image := marshal(map[string]any{"image": true})
	report := marshal(map[string]any{"review": true})
	changed := marshal(map[string]any{"mask": true})
	sourceRef, imageRef, reportRef, changedRef := ref(source), ref(image), ref(report), ref(changed)
	root := marshal(ImageResultDocument{SchemaVersion: 1, WorkingImage: imageRef,
		Report: reportRef, ChangedMask: changedRef})
	rootRef := ref(root)
	params := marshal(map[string]any{"base": v2input.SessionVersionBinding{
		AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: sourceRef.SHA256}})
	scope := RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: rootRef.ArtifactID, RootSHA: rootRef.SHA256}
	catalog := &restorationDocumentCatalogFixture{objects: map[string]objectstore.Object{}, reads: map[string]int{}}
	objects := restorationDocumentObjectsFixture{}
	for _, value := range []struct {
		role string
		ref  v2input.Ref
		body []byte
	}{
		{"source", sourceRef, source}, {"result", rootRef, root}, {"image", imageRef, image},
		{"report", reportRef, report}, {"changed", changedRef, changed},
	} {
		catalog.objects[value.role] = objectstore.Object{Key: value.role, VersionID: "version-1",
			SHA256: value.ref.SHA256, Bytes: int64(len(value.body)), ContentType: "application/json"}
		objects[value.role] = value.body
	}
	docs, err := LoadRestorationResultDocuments(context.Background(), scope, catalog, objects, params)
	if err != nil || !bytes.Equal(docs.Result, root) || !bytes.Equal(docs.Source, source) ||
		!bytes.Equal(docs.Image, image) || !bytes.Equal(docs.Report, report) ||
		!bytes.Equal(docs.Changed, changed) || docs.Generated != nil {
		t.Fatal("canonical document set refused", err)
	}
	catalog.reads = map[string]int{}
	catalog.change = true
	if _, err := LoadRestorationResultDocuments(context.Background(), scope, catalog, objects, params); err == nil {
		t.Fatal("changed document version accepted")
	}
	if restorationDocumentRole("task_params") || !restorationDocumentRole("generated") {
		t.Fatal("document role allowlist changed")
	}
}
