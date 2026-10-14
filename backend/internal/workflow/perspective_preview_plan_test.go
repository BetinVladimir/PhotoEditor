package workflow

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

type perspectivePlanFixture struct {
	pin  v2render.PerspectivePreviewSource
	docs map[string][]byte
}

func (f *perspectivePlanFixture) Resolve(context.Context, uuid.UUID, uuid.UUID) (v2render.PerspectivePreviewSource, error) {
	return f.pin, nil
}
func (f *perspectivePlanFixture) ResolveObject(_ context.Context, _ v2render.PerspectivePreviewSource, n v2draft.LayerRef) (objectstore.Object, error) {
	raw, ok := f.docs[n.Ref.ArtifactID.String()]
	if !ok {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return objectstore.Object{Key: n.Ref.ArtifactID.String(), VersionID: "immutable", SHA256: n.Ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}, nil
}
func (f *perspectivePlanFixture) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.docs[o.Key])), nil
}
func perspectivePlanBundle(t *testing.T) v2render.PerspectivePreviewBundle {
 return perspectivePlanBundleWithSource(t, nil)
}
func perspectivePlanBundleWithSource(t *testing.T, change func(*v2render.PerspectivePreviewSource)) v2render.PerspectivePreviewBundle {
	t.Helper()
	f := &perspectivePlanFixture{docs: map[string][]byte{}}
	put := func(v any) v2input.Ref {
		raw, err := canonical.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		r := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(raw)}
		f.docs[r.ArtifactID.String()] = raw
		return r
	}
	chainHash, err := canonical.Hash(map[string]any{"steps": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := canonical.Marshal(map[string]any{"steps": []any{}, "hash": chainHash})
	if err != nil {
		t.Fatal(err)
	}
	canvas := review.Canvas{Width: 4, Height: 3, Orientation: 1, SpaceID: "source", SourceSHA256: strings.Repeat("a", 64)}
	image := review.WorkingImage{Pixels: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Canvas: canvas, Color: review.ColorSpace{ProfileID: "linear_srgb_v1", Encoding: "linear", ICCSHA256: strings.Repeat("c", 64)}, DType: "float32", TransformChain: chain}
	source := put(image)
	canvasHash, err := canonical.Hash(canvas)
	if err != nil {
		t.Fatal(err)
	}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvasHash}
	mask := put(review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Canvas: canvas, TransformChain: chain, Selector: "changed", Confidence: 1})
	report := put(map[string]any{"schema_version": 1, "status": "pass", "operation": "optical.perspective", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []v2input.SessionVersionBinding{base}, "registrations": []v2input.Ref{}})
	root := put(v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: source, ChangedMask: mask, Report: report})
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := v2input.TaskID(workflow, "optical.perspective", 0)
	if err != nil {
		t.Fatal(err)
	}
	f.pin = v2render.PerspectivePreviewSource{Tenant: uuid.New(), Project: uuid.New(), Run: run, Workflow: workflow, Producer: producer, ResultID: uuid.New(), Result: root, Attempt: 1, Fence: 1, Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Base: base}
	if change != nil { change(&f.pin) }
	bundle, err := v2render.LoadPerspectivePreviewBundle(context.Background(), f, f, f, f.pin.Tenant, f.pin.Producer)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}
func TestPerspectivePreviewPlanUsesExistingMediaParameters(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	srgb := strings.Repeat("e", 64)
	plan, err := CompilePerspectivePreviewPlan(bundle, srgb)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CompilePerspectivePreviewPlan(bundle, srgb)
	if err != nil || again.Run() != plan.Run() {
		t.Fatal("unstable preview run", err)
	}
	if plan.Run() == bundle.Source().Run || plan.Source() != bundle.Source() || plan.SRGBSHA() != srgb {
		t.Fatal("lost parent provenance")
	}
	docs := plan.Documents()
	if len(docs) != 2 {
		t.Fatal("missing variants")
	}
	for _, d := range docs {
		if d.Run != plan.Run() {
			t.Fatal("foreign child run")
		}
		task, err := PrepareRenderDerivativeTask(d, review.ArtifactRef{ID: uuid.New(), SHA256: d.ParamsSHA})
		if err != nil || task.Operation != "media.preview" {
			t.Fatal("invalid existing media ABI", err)
		}
	}
	docs[0].Params[0] = 'x'
	docs[0].Inputs[0].SHA256 = "mutated"
	if plan.Documents()[0].Params[0] == 'x' || plan.Documents()[0].Inputs[0].SHA256 == "mutated" {
		t.Fatal("borrowed mutable plan")
	}
	different, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("f", 64))
	if err != nil || different.Run() == plan.Run() {
		t.Fatal("ICC profile change reused run", err)
	}
}
func TestPerspectivePreviewPlanRefusesMissingBundle(t *testing.T) {
	if _, err := CompilePerspectivePreviewPlan(v2render.PerspectivePreviewBundle{}, strings.Repeat("e", 64)); err == nil {
		t.Fatal("empty bundle accepted")
	}
	if _, err := CompilePerspectivePreviewPlan(v2render.PerspectivePreviewBundle{}, "bad"); err == nil {
		t.Fatal("invalid ICC accepted")
	}
}
