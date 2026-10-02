package v2learning

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2observation"
)

func faceFloat(value float64) *float64 { return &value }

func TestFrozenFacesRequireExactBindingAndConsentEmbeddingPair(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	face := v2observation.Face{ObservationID: uuid.New(), FaceID: uuid.New(),
		Box: &v2observation.FaceRect{X0: faceFloat(.1), Y0: faceFloat(.1),
			X1: faceFloat(.9), Y1: faceFloat(.9)},
		Quality: faceFloat(.8), Landmarks: make([]v2observation.FacePoint, 68)}
	for i := range face.Landmarks {
		face.Landmarks[i] = v2observation.FacePoint{X: faceFloat(.5), Y: faceFloat(.5)}
	}
	evidence := v2observation.Evidence{SchemaVersion: 1,
		Binding: v2observation.Binding{AssetVersionID: binding.AssetVersionID,
			Source: v2observation.ArtifactRef{ArtifactID: binding.Source.ArtifactID,
				SHA256: binding.Source.SHA256}, CanvasHash: binding.CanvasHash},
		Faces: []v2observation.Face{face}}
	body, err := canonical.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := ValidateFrozenFaceObservations(body, binding); err != nil || count != 1 {
		t.Fatalf("valid frozen face rejected: %v", err)
	}
	other := binding
	other.AssetVersionID = uuid.New()
	if _, err := ValidateFrozenFaceObservations(body, other); err == nil {
		t.Fatal("foreign face binding accepted")
	}
	face.Embedding = make([]float64, 512)
	evidence.Faces[0] = face
	body, _ = canonical.Marshal(evidence)
	if _, err := ValidateFrozenFaceObservations(body, binding); err == nil {
		t.Fatal("embedding without confirmed person accepted")
	}
	person := uuid.New()
	face.PersonID = &person
	evidence.Faces[0] = face
	body, _ = canonical.Marshal(evidence)
	if count, err := ValidateFrozenFaceObservations(body, binding); err != nil || count != 1 {
		t.Fatalf("confirmed face rejected: %v", err)
	}
	evidence.Faces = []v2observation.Face{face, face}
	body, _ = canonical.Marshal(evidence)
	if _, err := ValidateFrozenFaceObservations(body, binding); err == nil {
		t.Fatal("duplicate face accepted")
	}
}
