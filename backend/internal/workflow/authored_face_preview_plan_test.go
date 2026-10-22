package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

func authoredFacePreviewFixture(t *testing.T) (v2render.AuthoredFaceSelectionCandidate, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("wire fixture")
	}
	body, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	c := v2render.AuthoredFaceSelectionCandidate{Tenant: uuid.New(), Parent: uuid.New(), Instruction: uuid.New(), Ordinal: 1, Source: v2input.SessionVersionBinding{Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(body)}, CanvasHash: canvas}}
	return c, body
}
func TestAuthoredFacePreviewUsesActualCurrentWorkingImage(t *testing.T) {
	c, raw := authoredFacePreviewFixture(t)
	body, _ := canonical.Marshal(c)
	doc, err := CompileAuthoredFacePreviewDocument(renderDerivativeBytesSHA(body), c, raw, strings.Repeat("b", 64), media.CurrentRendererPolicy)
	if err != nil {
		t.Fatal(err)
	}
	task, err := PrepareAuthoredFacePreviewTask(doc, review.ArtifactRef{ID: uuid.New(), SHA256: doc.ParamsSHA})
	if err != nil || task.Operation != "media.preview" || task.TimeoutSeconds != 300 || task.ResourceClass != "media" || doc.Inputs[0].ID != c.Source.Source.ArtifactID {
		t.Fatal("current source presentation", err)
	}
}
func TestAuthoredFacePreviewRefusesSubstitutedCanvasAndNilSource(t *testing.T) {
	for _, kind := range []string{"canvas", "source"} {
		t.Run(kind, func(t *testing.T) {
			c, raw := authoredFacePreviewFixture(t)
			if kind == "canvas" {
				c.Source.CanvasHash = strings.Repeat("f", 64)
			} else {
				c.Source.Source.ArtifactID = uuid.Nil
			}
			body, _ := canonical.Marshal(c) // recompute candidate hash: refusal must be geometric/identity, not stale hash.
			if _, err := CompileAuthoredFacePreviewDocument(renderDerivativeBytesSHA(body), c, raw, strings.Repeat("b", 64), media.CurrentRendererPolicy); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}
