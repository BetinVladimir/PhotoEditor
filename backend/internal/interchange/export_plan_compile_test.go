package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type exportFrameResolverFunc func(context.Context, v2input.SessionSnapshotItem) (ExportFrame, error)

func (f exportFrameResolverFunc) ResolveExportFrame(ctx context.Context, item v2input.SessionSnapshotItem) (ExportFrame, error) {
	return f(ctx, item)
}
func exportPlanFixture(t *testing.T) (uuid.UUID, uuid.UUID, v2input.Ref, []byte, ExportFrame) {
	t.Helper()
	ref := func(text string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(text))}
	}
	master := ref("master")
	item := v2input.SessionSnapshotItem{Ordinal: 0, Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: master, CanvasHash: rawSHA([]byte("canvas"))}, WorkingImage: master, Recipe: ref("frozen")}
	_, raw, err := v2input.CompilePinnedSessionInputSnapshot(uuid.New(), []v2input.SessionSnapshotItem{item})
	if err != nil {
		t.Fatal(err)
	}
	frame := ExportFrame{ExternalID: "one", Path: "images/one.tiff", Content: ref("image"), PortableRecipe: ref("portable"), Rating: 4, Pick: "picked"}
	return uuid.New(), uuid.New(), v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}, raw, frame
}

func TestExportPlanOwnsDocumentsAndExactSnapshotBinding(t *testing.T) {
	workflow, project, snapshot, raw, frame := exportPlanFixture(t)
	resolver := exportFrameResolverFunc(func(_ context.Context, item v2input.SessionSnapshotItem) (ExportFrame, error) { return frame, nil })
	limits := ExportLimits{ArchiveBytes: 1 << 20, ExpandedBytes: 1 << 20, Files: 10}
	first, err := CompileExportPlan(context.Background(), workflow, project, "photoeditor_bundle", snapshot, raw, limits, resolver)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileExportPlan(context.Background(), workflow, project, "photoeditor_bundle", snapshot, raw, limits, resolver)
	if err != nil {
		t.Fatal(err)
	}
	pref, body := first.PlanDocument()
	other, otherBody := second.PlanDocument()
	if pref != other || !bytes.Equal(body, otherBody) || first.TaskID() != uuid.NewSHA1(workflow, []byte("v2.interchange.export:0")) {
		t.Fatal("non-deterministic export")
	}
	var plan importPlanDocument
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Direction != "export" || plan.Entries[0].Recipe == nil || plan.Entries[0].Binding == nil || plan.Entries[0].Content != frame.Content {
		t.Fatal("lost frozen identity")
	}
	body[0] = 'x'
	_, owned := first.PlanDocument()
	if owned[0] != '{' {
		t.Fatal("document alias")
	}
	roots := first.InputRoots()
	if len(roots) != 7 {
		t.Fatalf("roots=%d", len(roots))
	}
	roots[0].Contract = "other"
	if first.InputRoots()[0].Contract != "InputSnapshot" {
		t.Fatal("root alias")
	}
	paramsRef, params := first.ParamsDocument()
	if paramsRef.SHA256 != rawSHA(params) {
		t.Fatal("params hash")
	}
}

func TestExportPlanRejectsInvalidEvidenceBeforeResolver(t *testing.T) {
	for _, mode := range []string{"hash", "snapshot", "format", "quota"} {
		t.Run(mode, func(t *testing.T) {
			workflow, project, snapshot, raw, frame := exportPlanFixture(t)
			calls := 0
			format := "xmp"
			limits := ExportLimits{ArchiveBytes: 1 << 20, ExpandedBytes: 1 << 20, Files: 10}
			if mode == "hash" {
				snapshot.SHA256 = rawSHA([]byte("wrong"))
			}
			if mode == "snapshot" {
				raw = []byte(`{"schema_version":1}`)
				snapshot.SHA256 = rawSHA(raw)
			}
			if mode == "format" {
				format = "psd"
			}
			if mode == "quota" {
				limits.ArchiveBytes = 0
			}
			_, err := CompileExportPlan(context.Background(), workflow, project, format, snapshot, raw, limits, exportFrameResolverFunc(func(context.Context, v2input.SessionSnapshotItem) (ExportFrame, error) { calls++; return frame, nil }))
			if err == nil || calls != 0 {
				t.Fatal("invalid evidence reached resolver", err, calls)
			}
		})
	}
}

func TestExportPlanRejectsTypedArtifactAlias(t *testing.T) {
	workflow, project, snapshot, raw, frame := exportPlanFixture(t)
	resolver := exportFrameResolverFunc(func(_ context.Context, item v2input.SessionSnapshotItem) (ExportFrame, error) {
		frame.Content = item.WorkingImage
		return frame, nil
	})
	if _, err := CompileExportPlan(context.Background(), workflow, project, "xmp", snapshot, raw, ExportLimits{1 << 20, 1 << 20, 10}, resolver); err == nil {
		t.Fatal("encoded image aliased master")
	}
}

func TestExportPlanFreezesColorLabelWithoutChangingLegacyBytes(t *testing.T) {
	workflow, project, snapshot, raw, frame := exportPlanFixture(t)
	compile := func() (CompiledExportPlan, error) {
		return CompileExportPlan(context.Background(), workflow, project, "xmp", snapshot, raw, ExportLimits{1 << 20, 1 << 20, 10}, exportFrameResolverFunc(func(context.Context, v2input.SessionSnapshotItem) (ExportFrame, error) { return frame, nil }))
	}
	legacy, err := compile()
	if err != nil {
		t.Fatal(err)
	}
	_, legacyBody := legacy.PlanDocument()
	label := "Blue"
	frame.Label = &label
	withLabel, err := compile()
	if err != nil {
		t.Fatal(err)
	}
	_, labeledBody := withLabel.PlanDocument()
	if bytes.Equal(legacyBody, labeledBody) {
		t.Fatal("label did not change frozen plan")
	}
	var plan importPlanDocument
	if err := json.Unmarshal(labeledBody, &plan); err != nil || plan.Entries[0].Label == nil || *plan.Entries[0].Label != "Blue" {
		t.Fatal("label missing from plan", err)
	}
	label = "Red"
	_, owned := withLabel.PlanDocument()
	if !bytes.Equal(labeledBody, owned) {
		t.Fatal("caller mutated frozen label")
	}
	invalid := "Custom"
	frame.Label = &invalid
	if _, err := compile(); err == nil {
		t.Fatal("unsupported label admitted")
	}
}
