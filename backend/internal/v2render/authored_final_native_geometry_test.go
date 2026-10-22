package v2render

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Reuses the real current perspective geometry validator's dense-source fixture.
// Content fixtures prove predicates only; no SQL adoption/photographic claim.
func authoredNativePerspectiveContent(t *testing.T, mutation string) (AuthoredPhaseReceipt, map[v2input.Ref][]byte) {
	t.Helper()
	binding, result, source, docs := perspectiveGeometryFixture(t)
	// The helper historically validates image content; this test publishes its typed root.
	result.SchemaVersion = 1
	var report map[string]json.RawMessage
	if err := json.Unmarshal(docs[result.Report.ArtifactID.String()], &report); err != nil {
		t.Fatal(err)
	}
	var transform v2input.Ref
	if err := json.Unmarshal(report["transform"], &transform); err != nil {
		t.Fatal(err)
	}
	if mutation == "transform" {
		var value map[string]any
		_ = json.Unmarshal(docs[transform.ArtifactID.String()], &value)
		value["matrix"] = []float64{1, 0, 2, 0, 1, 1, 0, 0, 1}
		docs[transform.ArtifactID.String()], _ = canonical.Marshal(value)
	}
	transform.SHA256, _ = canonical.Hash(json.RawMessage(docs[transform.ArtifactID.String()]))
	report["transform"], _ = canonical.Marshal(transform)
	docs[result.Report.ArtifactID.String()], _ = canonical.Marshal(report)
	if mutation == "canvas" || mutation == "source" || mutation == "chain" {
		var image review.WorkingImage
		_ = json.Unmarshal(docs[result.WorkingImage.ArtifactID.String()], &image)
		switch mutation {
		case "canvas":
			image.Canvas.Width++
		case "source":
			image.Canvas.SourceSHA256 = authoredLinesTestSHA
		case "chain":
			image.TransformChain = json.RawMessage(`{"steps":[],"hash":"` + authoredLinesTestSHA + `"}`)
		}
		docs[result.WorkingImage.ArtifactID.String()], _ = canonical.Marshal(image)
	}
	result.WorkingImage.SHA256, _ = canonical.Hash(json.RawMessage(docs[result.WorkingImage.ArtifactID.String()]))
	result.ChangedMask.SHA256, _ = canonical.Hash(json.RawMessage(docs[result.ChangedMask.ArtifactID.String()]))
	result.Report.SHA256, _ = canonical.Hash(json.RawMessage(docs[result.Report.ArtifactID.String()]))
	rootBody, _ := canonical.Marshal(result)
	rootSHA, _ := canonical.Hash(result)
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: rootSHA}
	bodies := map[v2input.Ref][]byte{root: rootBody, binding.Base.Source: source,
		result.WorkingImage: docs[result.WorkingImage.ArtifactID.String()], result.ChangedMask: docs[result.ChangedMask.ArtifactID.String()], result.Report: docs[result.Report.ArtifactID.String()], transform: docs[transform.ArtifactID.String()]}
	phase := AuthoredPhaseReceipt{InputSource: binding.Base, OutputSource: binding.Base, Geometry: &AuthoredGeometryPhaseWitness{Task: AuthoredPhaseTask{ResultID: uuid.New(), Operation: "optical.perspective"}, Root: root, Image: result.WorkingImage, Report: result.Report}}
	// The authored blend output is a distinct occurrence on a different canvas.
	phase.OutputSource.Source = v2input.Ref{ArtifactID: uuid.New(), SHA256: authoredLinesTestSHA}
	phase.OutputSource.CanvasHash = authoredLinesTestSHA
	if mutation == "root" {
		phase.Geometry.Root = phase.Geometry.Image
	}
	if mutation == "image" {
		phase.Geometry.Image = phase.OutputSource.Source
	}
	return phase, bodies
}
func TestAuthoredNativeGeometryIsIndependentOfBlendTarget(t *testing.T) {
	phase, bodies := authoredNativePerspectiveContent(t, "")
	geometry, err := authoredNativeSingleGeometry(phase, bodies)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.CanvasHash == phase.OutputSource.CanvasHash || geometry.CanvasHash == phase.InputSource.CanvasHash || geometry.TransformHash == "" {
		t.Fatal("native geometry replaced by input or blend canvas")
	}
}
func TestAuthoredNativeGeometrySubstitutionRefused(t *testing.T) {
	for _, name := range []string{"canvas", "source", "chain", "transform", "root", "image"} {
		t.Run(name, func(t *testing.T) {
			phase, bodies := authoredNativePerspectiveContent(t, name)
			if _, err := authoredNativeSingleGeometry(phase, bodies); err == nil {
				t.Fatal("native substitution accepted")
			}
		})
	}
}

// Keep result decoder in this test's compile closure: a root must remain the
// actual typed ImageResult, never a WorkingImage under an alias.
var _ = v2draft.ParseImageResult
