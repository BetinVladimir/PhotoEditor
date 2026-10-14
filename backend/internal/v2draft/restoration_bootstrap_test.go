package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type restorationBootstrapDB struct {
	tx     *restorationBootstrapTx
	begins int
}

func (d *restorationBootstrapDB) Begin(context.Context) (pgx.Tx, error) { d.begins++; return d.tx, nil }

type restorationBootstrapTx struct {
	*restorationOriginTx
	commits int
}

func (t *restorationBootstrapTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *restorationBootstrapTx) Rollback(context.Context) error { return nil }

type restorationRevokingCatalog struct {
	*restorationCatalog
	revoke uuid.UUID
	after  int
}

func (c *restorationRevokingCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	node LayerRef) (objectstore.Object, error) {
	obj, err := c.restorationCatalog.Lookup(ctx, who, tenant, node)
	if err == nil && node.Ref.ArtifactID == c.revoke && c.calls[c.revoke] > c.after {
		obj.VersionID = "revoked"
	}
	return obj, err
}

func TestBootstrapRestorationManualMasksPinsVersionAndGeometry(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(fixture, &images); err != nil || len(images) == 0 {
		t.Fatal("fixture", err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(sourceRaw)}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}
	target := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	guard := target
	guard.Artifact.ID = uuid.New()
	guard.Protected = true
	targetRaw, err := canonical.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	guardRaw, err := canonical.Marshal(guard)
	if err != nil {
		t.Fatal(err)
	}
	targetRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(targetRaw)}
	guardRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(guardRaw)}
	store := restorationObjects{}
	objects := map[uuid.UUID]objectstore.Object{}
	for ref, raw := range map[v2input.Ref][]byte{source: sourceRaw, targetRef: targetRaw, guardRef: guardRaw} {
		key := ref.ArtifactID.String()
		store[key] = raw
		objects[ref.ArtifactID] = objectstore.Object{Key: key, VersionID: "v1", SHA256: ref.SHA256,
			Bytes: int64(len(raw)), ContentType: "application/json"}
	}
	catalog := &restorationCatalog{objects: objects, calls: map[uuid.UUID]int{}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	got, err := BootstrapRestorationManualMasks(context.Background(), who, tenant, base,
		targetRef, guardRef, catalog, store)
	if err != nil || !bytes.Equal(got.Source, sourceRaw) || !bytes.Equal(got.Target, targetRaw) ||
		!bytes.Equal(got.Protected, guardRaw) {
		t.Fatal("bootstrap changed exact documents", err)
	}
	for _, ref := range []v2input.Ref{source, targetRef, guardRef} {
		if catalog.calls[ref.ArtifactID] != 2 {
			t.Fatal("locator was not rechecked", ref)
		}
	}
	catalog.calls = map[uuid.UUID]int{}
	catalog.change = guardRef.ArtifactID
	if _, err := BootstrapRestorationManualMasks(context.Background(), who, tenant, base,
		targetRef, guardRef, catalog, store); err == nil {
		t.Fatal("changed mask version accepted")
	}
	catalog.calls = map[uuid.UUID]int{}
	catalog.change = uuid.Nil
	if _, err := BootstrapRestorationManualMasks(context.Background(), who, tenant, base,
		guardRef, targetRef, catalog, store); err == nil {
		t.Fatal("target/protected roles swapped")
	}
}

func TestResolveRestorationManualMaterialRechecksAfterSQL(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(sourceRaw)}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}
	target := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	guard := target
	guard.Artifact.ID = uuid.New()
	guard.Protected = true
	targetRaw, err := canonical.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	guardRaw, err := canonical.Marshal(guard)
	if err != nil {
		t.Fatal(err)
	}
	targetRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(targetRaw)}
	guardRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(guardRaw)}
	params, err := canonical.Marshal(map[string]any{"base": base, "mask": targetRef, "protected": guardRef})
	if err != nil {
		t.Fatal(err)
	}
	store := restorationObjects{}
	objects := map[uuid.UUID]objectstore.Object{}
	for ref, raw := range map[v2input.Ref][]byte{source: sourceRaw, targetRef: targetRaw, guardRef: guardRaw} {
		key := ref.ArtifactID.String()
		store[key] = raw
		objects[ref.ArtifactID] = objectstore.Object{Key: key, VersionID: "v1", SHA256: ref.SHA256,
			Bytes: int64(len(raw)), ContentType: "application/json"}
	}
	catalog := &restorationRevokingCatalog{restorationCatalog: &restorationCatalog{
		objects: objects, calls: map[uuid.UUID]int{}}, revoke: guardRef.ArtifactID, after: 4}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	db := &restorationBootstrapDB{tx: &restorationBootstrapTx{
		restorationOriginTx: &restorationOriginTx{role: "photo_scheduler", root: root, original: original}}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	resolved, err := ResolveRestorationManualEvidence(context.Background(), db, who, tenant, project, snapshot,
		"image.reflection", params, catalog, nil, store)
	docs := resolved.Documents
	if err != nil || !bytes.Equal(docs.Source, sourceRaw) || !bytes.Equal(docs.Target, targetRaw) ||
		!bytes.Equal(docs.Protected, guardRaw) || resolved.Origin.Root != root ||
		resolved.Origin.Params != original ||
		resolved.Origin.TargetPixel.ArtifactID != target.Artifact.ID ||
		resolved.Origin.ProtectedPixel.ArtifactID != guard.Artifact.ID ||
		db.begins != 1 || db.tx.commits != 1 ||
		catalog.calls[guardRef.ArtifactID] != 4 {
		t.Fatal("material origin path did not complete", err)
	}
	catalog.calls = map[uuid.UUID]int{}
	catalog.after = 3 // bootstrap and scoped read succeed, but final locator recheck changes
	if _, err := ResolveRestorationManualMaterial(context.Background(), db, who, tenant, project, snapshot,
		"image.reflection", params, catalog, nil, store); err == nil {
		t.Fatal("post-SQL locator change accepted")
	}
}
