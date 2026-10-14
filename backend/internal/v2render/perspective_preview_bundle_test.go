package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type previewBundleFixture struct {
	pin         PerspectivePreviewSource
	docs        map[string][]byte
	sourceReads int
	changed     bool
	revoked     bool
	opens       int
}

func (f *previewBundleFixture) Resolve(context.Context, uuid.UUID, uuid.UUID) (PerspectivePreviewSource, error) {
	f.sourceReads++
	if f.revoked && f.sourceReads > 1 {
		return PerspectivePreviewSource{}, v2input.ErrExpired
	}
	pin := f.pin
	if f.changed && f.sourceReads > 1 {
		pin.Fence++
	}
	return pin, nil
}
func (f *previewBundleFixture) ResolveObject(_ context.Context, _ PerspectivePreviewSource, node v2draft.LayerRef) (objectstore.Object, error) {
	raw, ok := f.docs[node.Ref.ArtifactID.String()]
	if !ok {
		return objectstore.Object{}, v2input.ErrExpired
	}
	return objectstore.Object{Key: node.Ref.ArtifactID.String(), VersionID: "immutable", SHA256: node.Ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}, nil
}
func (f *previewBundleFixture) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	f.opens++
	return io.NopCloser(bytes.NewReader(f.docs[obj.Key])), nil
}

func newPreviewBundleFixture(t *testing.T) *previewBundleFixture {
	t.Helper()
	binding, result, sourceRaw, docs := perspectiveGeometryFixture(t)
	pin := perspectivePreviewSourceFixture(t)
	pin.Base = binding.Base
	docs[pin.Base.Source.ArtifactID.String()] = sourceRaw
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	result.SchemaVersion = 1
	result.WorkingImage.SHA256 = digest(docs[result.WorkingImage.ArtifactID.String()])
	result.ChangedMask.SHA256 = digest(docs[result.ChangedMask.ArtifactID.String()])
	var report map[string]any
	if err := json.Unmarshal(docs[result.Report.ArtifactID.String()], &report); err != nil {
		t.Fatal(err)
	}
	var transform v2input.Ref
	raw, err := canonical.Marshal(report["transform"])
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &transform); err != nil {
		t.Fatal(err)
	}
	transform.SHA256 = digest(docs[transform.ArtifactID.String()])
	report["transform"] = transform
	docs[result.Report.ArtifactID.String()], err = canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	result.Report.SHA256 = digest(docs[result.Report.ArtifactID.String()])
	docs[pin.Result.ArtifactID.String()], err = canonical.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	pin.Result.SHA256 = digest(docs[pin.Result.ArtifactID.String()])
	return &previewBundleFixture{pin: pin, docs: docs}
}

func TestPerspectivePreviewBundleOwnsVerifiedDocuments(t *testing.T) {
	f := newPreviewBundleFixture(t)
	bundle, err := LoadPerspectivePreviewBundle(context.Background(), f, f, f, f.pin.Tenant, f.pin.Producer)
	if err != nil {
		t.Fatal(err)
	}
	if f.sourceReads != 2 || f.opens != 6 || len(bundle.Documents()) != 6 {
		t.Fatal("incomplete bundle", f.sourceReads, f.opens, len(bundle.Documents()))
	}
	exported := bundle.Documents()
	exported[f.pin.Result.ArtifactID.String()][0] = 'x'
	delete(exported, f.pin.Base.Source.ArtifactID.String())
	if bundle.Documents()[f.pin.Result.ArtifactID.String()][0] == 'x' || len(bundle.Documents()) != 6 {
		t.Fatal("bundle storage escaped")
	}
}
func TestPerspectivePreviewBundleRefusesFinalProducerChange(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		f := newPreviewBundleFixture(t)
		f.changed = !revoke
		f.revoked = revoke
		if _, err := LoadPerspectivePreviewBundle(context.Background(), f, f, f, f.pin.Tenant, f.pin.Producer); err != v2input.ErrExpired {
			t.Fatal("producer change accepted", err)
		}
	}
}
func TestPerspectivePreviewBundleCancellationBeforeIO(t *testing.T) {
	f := newPreviewBundleFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadPerspectivePreviewBundle(ctx, f, f, f, f.pin.Tenant, f.pin.Producer); err != context.Canceled || f.opens != 0 || f.sourceReads != 0 {
		t.Fatal("cancelled bundle reached IO", err)
	}
}
