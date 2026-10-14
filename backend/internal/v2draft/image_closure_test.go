package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
	"testing"
)

type imageClosureCatalog struct {
	closureFixture
	lookups    []LayerRef
	reads      int
	denyParent bool
	denyPath   string
	geometries map[uuid.UUID]DocumentGeometry
}

func (f *imageClosureCatalog) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	f.reads++
	return f.closureFixture.Open(ctx, obj)
}

func (f *imageClosureCatalog) Geometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (DocumentGeometry, bool, error) {
	geometry, found := f.geometries[node.Ref.ArtifactID]
	return geometry, found, nil
}

func (f *imageClosureCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	f.lookups = append(f.lookups, node)
	if f.denyPath != "" && node.Path == f.denyPath || f.denyParent && node.Contract == "Calibration" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return f.closureFixture.Lookup(ctx, who, tenant, node)
}

func TestScopedImageClosureTraversesAllTypedEvidenceAndRechecksOccurrences(t *testing.T) {
	f := &imageClosureCatalog{closureFixture: closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}}
	put := func(value any) v2input.Ref {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		key := ref.ArtifactID.String()
		f.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: ref.SHA256, Bytes: int64(len(body)), VersionID: "version-1", ContentType: "application/json"}
		f.bodies[key] = body
		return ref
	}
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("image fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	source := put(images[0])
	canvasSHA, _ := canonical.Hash(image.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvasSHA}
	pixel := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	f.objects[pixel.ArtifactID] = objectstore.Object{Key: "pixels", SHA256: pixel.SHA256, Bytes: 10, VersionID: "version-1", ContentType: "application/octet-stream"}
	maskPixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	f.objects[maskPixels.ArtifactID] = objectstore.Object{Key: "mask", SHA256: maskPixels.SHA256, Bytes: 10, VersionID: "version-1", ContentType: "application/octet-stream"}
	mask := put(map[string]any{"artifact": maskPixels, "canvas": image.Canvas, "transform_chain": image.TransformChain, "object_id": nil, "selector": "generated", "confidence": 1, "model_lock_id": nil, "protected": false})
	calibration := put(map[string]any{"fixture": true})
	identity := put(map[string]any{"schema_version": 1, "status": "not_measurable", "faces": []any{}, "changed_mask": mask, "generated_mask": mask, "outside_mask_max_error": 0, "calibration": calibration})
	parentReport := put(map[string]any{"schema_version": 1, "status": "review", "operation": "parent.fixture", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []v2input.SessionVersionBinding{binding}, "registrations": []any{}})
	report := put(map[string]any{"schema_version": 1, "status": "review", "operation": "fixture", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": identity, "transform": nil, "sources": []v2input.SessionVersionBinding{binding}, "registrations": []any{}, "parent_reports": []v2input.Ref{parentReport}})
	result := put(map[string]any{"schema_version": 1, "working_image": source, "changed_mask": mask, "generated_mask": mask, "report": report})
	children, err := DocumentChildren(f.bodies[result.ArtifactID.String()], "ImageResult", result)
	if err != nil || len(children) != 4 {
		t.Fatal("result inventory children", err)
	}
	for i, want := range []string{"Document.changed_mask", "Document.generated_mask", "Document.report", "Document.working_image"} {
		if children[i].Path != want || children[i].ParentRef != result {
			t.Fatal("schema first-visit ordering", children)
		}
	}
	for _, contract := range []string{"ImageResult", "ImageReport", "WorkingImage", "MaskRef", "Artifact"} {
		if _, err := DocumentChildren([]byte(`{"invalid":true}`), contract, result); err == nil {
			t.Fatal("invalid inventory document", contract)
		}
	}
	roots := []LayerRef{{Path: "source", Contract: "WorkingImage", Ref: source}, {Path: "result", Contract: "ImageResult", Ref: result}}
	resolver := ClosureResolver{Catalog: f, Objects: f}
	refs, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding)
	if err != nil {
		t.Fatal(err, f.lookups)
	}
	expected := map[uuid.UUID]string{source.ArtifactID: "WorkingImage", pixel.ArtifactID: "WorkingPixels", mask.ArtifactID: "MaskRef", maskPixels.ArtifactID: "MaskPixels", calibration.ArtifactID: "Calibration", identity.ArtifactID: "IdentityReport", report.ArtifactID: "ImageReport", parentReport.ArtifactID: "ImageReport", result.ArtifactID: "ImageResult"}
	seen := map[uuid.UUID]bool{}
	sourceOccurrences := 0
	for _, node := range f.lookups {
		if expected[node.Ref.ArtifactID] != node.Contract {
			t.Fatalf("wrong dependency typing %+v", node)
		}
		seen[node.Ref.ArtifactID] = true
		if node.Ref == source {
			sourceOccurrences++
		}
	}
	if len(seen) != len(expected) || len(refs)+f.reads != len(f.lookups) || sourceOccurrences < 3 {
		t.Fatal("missing dependency/occurrence authorization", len(refs), len(seen), sourceOccurrences)
	}
	chainSHA, _ := canonical.Hash(image.TransformChain)
	f.geometries = map[uuid.UUID]DocumentGeometry{mask.ArtifactID: {CanvasHash: canvasSHA, TransformHash: chainSHA}}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding); err != nil {
		t.Fatal("verified result geometry", err)
	}
	for _, bad := range []DocumentGeometry{{CanvasHash: canvasSHA, TransformHash: strings.Repeat("f", 64)}, {CanvasHash: strings.Repeat("f", 64), TransformHash: chainSHA}, {CanvasHash: "invalid", TransformHash: chainSHA}} {
		f.geometries[mask.ArtifactID] = bad
		if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding); err == nil {
			t.Fatal("mask geometry mismatch accepted", bad)
		}
	}
	f.geometries = map[uuid.UUID]DocumentGeometry{source.ArtifactID: {CanvasHash: canvasSHA, TransformHash: strings.Repeat("f", 64)}}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding); err == nil {
		t.Fatal("image chain mismatch accepted")
	}
	f.geometries = nil
	wrongCanvas := append([]LayerRef(nil), roots...)
	wrongCanvas = append(wrongCanvas, LayerRef{Path: "repeated-source", Contract: "WorkingImage", Ref: source, CanvasHash: strings.Repeat("f", 64)})
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), wrongCanvas, binding); err == nil {
		t.Fatal("repeated image bypassed canvas check")
	}
	malformed := put(map[string]any{"not_an_image": true})
	promotion := []LayerRef{{Path: "generic", Contract: "Artifact", Ref: malformed}, {Path: "typed", Contract: "WorkingImage", Ref: malformed}}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), promotion, binding); err == nil {
		t.Fatal("generic artifact bypassed typed image validation")
	}
	inventory, err := resolver.ResolveTaskInputs(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: result, Contract: "ImageResult"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	if err != nil || len(inventory) != len(expected) {
		t.Fatal("task inventory", inventory, err)
	}
	readsBefore := f.reads
	withEvidence, witnesses, err := resolver.ResolveTaskInputsWithWitnesses(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: result, Contract: "ImageResult"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	if err != nil || !reflect.DeepEqual(inventory, withEvidence) || len(witnesses) != f.reads-readsBefore || len(witnesses) == 0 {
		t.Fatal("witness collection changed inventory or repeated IO", err, len(witnesses), f.reads-readsBefore)
	}
	for _, witness := range witnesses {
		sum := sha256.Sum256([]byte(witness.JCS))
		if hex.EncodeToString(sum[:]) != witness.Ref.SHA256 || witness.Object != f.objects[witness.Ref.ArtifactID] || witness.Path == "" {
			t.Fatal("witness lost immutable byte/metadata identity", witness.Path)
		}
	}
	lookupsBefore := len(f.lookups)
	readsBefore = f.reads
	evidence, err := resolver.ResolveTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: result, Contract: "ImageResult"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	if err != nil || !reflect.DeepEqual(inventory, evidence.Entries) || len(evidence.Documents) != f.reads-readsBefore || len(evidence.Occurrences) != len(f.lookups)-lookupsBefore-(f.reads-readsBefore) {
		t.Fatal("full evidence changed traversal or lost an occurrence", err)
	}
	binaries := 0
	for _, occurrence := range evidence.Occurrences {
		if occurrence.Object != f.objects[occurrence.Node.Ref.ArtifactID] {
			t.Fatal("occurrence metadata changed")
		}
		if occurrence.Node.Contract == "WorkingPixels" || occurrence.Node.Contract == "MaskPixels" {
			binaries++
		}
	}
	if binaries == 0 || len(evidence.Occurrences) <= len(evidence.Entries) {
		t.Fatal("binary/repeated occurrences omitted")
	}
	f.denyParent = true
	failedEntries, failedWitnesses, err := resolver.ResolveTaskInputsWithWitnesses(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: result, Contract: "ImageResult"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	if err == nil || failedEntries != nil || failedWitnesses != nil {
		t.Fatal("failed closure leaked partial evidence")
	}
	failedEvidence, err := resolver.ResolveTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: result, Contract: "ImageResult"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	if err == nil || failedEvidence.Entries != nil || failedEvidence.Documents != nil || failedEvidence.Occurrences != nil {
		t.Fatal("full evidence leaked a failed partial traversal")
	}
	f.denyParent = false
	for i, entry := range inventory {
		want := expected[entry.Ref.ArtifactID]
		if want == "WorkingPixels" || want == "MaskPixels" {
			want = "NpyArray"
		}
		if entry.Contract != want || i > 0 && bytes.Compare(inventory[i-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0 {
			t.Fatal("invalid sorted task inventory", entry)
		}
	}
	if _, err := resolver.ResolveTaskInputs(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: result, Contract: "ImageResult"}, {Ref: mask, Contract: "MaskRef"}}, binding); err == nil {
		t.Fatal("missing source root accepted")
	}
	if _, err := resolver.ResolveTaskInputs(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: source, Contract: "WorkingImage"}, {Ref: mask, Contract: "Artifact"}}, binding); err == nil {
		t.Fatal("generic root accepted")
	}
	for _, media := range []string{"application/x-npy", "application/octet-stream", "application/json", "image/png", ""} {
		t.Run("binary-media-"+media, func(t *testing.T) {
			obj := f.objects[pixel.ArtifactID]
			obj.ContentType = media
			f.objects[pixel.ArtifactID] = obj
			_, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding)
			allowed := media == "application/x-npy" || media == "application/octet-stream"
			if (err == nil) != allowed {
				t.Fatalf("binary media %q: %v", media, err)
			}
		})
	}
	obj := f.objects[pixel.ArtifactID]
	obj.ContentType = "application/octet-stream"
	f.objects[pixel.ArtifactID] = obj
	f.denyParent = true
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding); err == nil {
		t.Fatal("nested scope rejection ignored")
	}
}
