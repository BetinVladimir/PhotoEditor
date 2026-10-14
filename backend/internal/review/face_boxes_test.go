package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestSourceBoundPythonFaceBoxes(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-boxes-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var original FaceBoxes
	if decodeExact(raw, &original) != nil {
		t.Fatal("Python fixture")
	}
	source := DefectSource{Image: original.Image}
	boxes, err := ParseFaceBoxes(raw, source)
	if err != nil || len(boxes.Faces) != 1 || boxes.AcceptedCount != 1 {
		t.Fatal(boxes, err)
	}
	cases := map[string]func(*FaceBoxes){
		"wrong source":       func(d *FaceBoxes) { d.Image.ID = uuid.New() },
		"duplicate object":   func(d *FaceBoxes) { d.Faces = append(d.Faces, d.Faces[0]); d.AcceptedCount = 2; d.CandidateCount = 2 },
		"zero object":        func(d *FaceBoxes) { d.Faces[0].ObjectID = uuid.Nil },
		"empty rectangle":    func(d *FaceBoxes) { d.Faces[0].BBox.X1 = d.Faces[0].BBox.X0 },
		"outside rectangle":  func(d *FaceBoxes) { d.Faces[0].BBox.Y0 = -.1 },
		"confidence":         func(d *FaceBoxes) { d.Faces[0].Confidence = 1.1 },
		"accepted count":     func(d *FaceBoxes) { d.AcceptedCount = 0 },
		"candidate count":    func(d *FaceBoxes) { d.CandidateCount = 0 },
		"preview dimensions": func(d *FaceBoxes) { d.PreviewWidth = 1025 },
		"wrong model":        func(d *FaceBoxes) { d.ModelID = "other" },
		"invalid lock":       func(d *FaceBoxes) { d.ModelLockID = "bad" },
		"wrong backend":      func(d *FaceBoxes) { d.Backend = "other" },
		"unknown warning":    func(d *FaceBoxes) { d.Warnings = []string{"other"} },
		"duplicate warning":  func(d *FaceBoxes) { d.Warnings = []string{"source_srgb_clipped", "source_srgb_clipped"} },
		"false no_faces":     func(d *FaceBoxes) { d.Warnings = []string{"no_faces"} },
		"alpha fraction":     func(d *FaceBoxes) { d.AlphaExcludedFraction = -1 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var d FaceBoxes
			if decodeExact(raw, &d) != nil {
				t.Fatal("fixture")
			}
			change(&d)
			body, err := canonical.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := ParseFaceBoxes(body, source); err == nil || got.Image.ID != uuid.Nil {
				t.Fatal("invalid detection accepted", got, err)
			}
		})
	}
	original.Faces = []FaceBox{}
	original.AcceptedCount = 0
	original.Warnings = []string{"no_faces"}
	body, _ := canonical.Marshal(original)
	if got, err := ParseFaceBoxes(body, source); err != nil || len(got.Faces) != 0 {
		t.Fatal("valid empty detection", got, err)
	}
	original.Warnings = []string{}
	body, _ = canonical.Marshal(original)
	if got, err := ParseFaceBoxes(body, source); err == nil || got.Image.ID != uuid.Nil {
		t.Fatal("missing no_faces accepted")
	}
}

func TestLandmarkDetectionOriginalHashAndCompleteObjects(t *testing.T) {
	boxesRaw, err := os.ReadFile("testdata/face-boxes-python.json")
	if err != nil {
		t.Fatal(err)
	}
	landmarksRaw, err := os.ReadFile("testdata/face-landmarks-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var landmarks faceLandmarksDocument
	var boxes FaceBoxes
	if decodeExact(landmarksRaw, &landmarks) != nil || decodeExact(boxesRaw, &boxes) != nil {
		t.Fatal("fixtures")
	}
	source := DefectSource{Image: landmarks.Image, Canvas: landmarks.Landmarks[0].Canvas, TransformChain: landmarks.Landmarks[0].TransformChain}
	bind := func() {
		sum := sha256.Sum256(boxesRaw)
		landmarks.Faces = ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		landmarksRaw, _ = canonical.Marshal(landmarks)
	}
	bind()
	if err := VerifyLandmarkDetection(landmarksRaw, landmarks.Faces, boxesRaw, source); err != nil {
		t.Fatal(err)
	}
	wrong := landmarks.Faces
	wrong.ID = uuid.New()
	if err := VerifyLandmarkDetection(landmarksRaw, wrong, boxesRaw, source); err == nil {
		t.Fatal("foreign ref accepted")
	}
	if err := VerifyLandmarkDetection(landmarksRaw, landmarks.Faces, append(append([]byte{}, boxesRaw...), ' '), source); err == nil {
		t.Fatal("different original detection bytes accepted")
	}
	boxes.Faces[0].ObjectID = uuid.New()
	boxesRaw, _ = canonical.Marshal(boxes)
	bind()
	if err := VerifyLandmarkDetection(landmarksRaw, landmarks.Faces, boxesRaw, source); err == nil {
		t.Fatal("undetected object accepted")
	}
	boxes.Faces = []FaceBox{}
	boxes.AcceptedCount = 0
	boxes.Warnings = []string{"no_faces"}
	boxesRaw, _ = canonical.Marshal(boxes)
	bind()
	if err := VerifyLandmarkDetection(landmarksRaw, landmarks.Faces, boxesRaw, source); err == nil {
		t.Fatal("cardinality mismatch accepted")
	}
	landmarks.Landmarks = []Face68{}
	landmarks.MeanConfidence = 0
	landmarks.Warnings = []string{"no_faces"}
	bind()
	if err := VerifyLandmarkDetection(landmarksRaw, landmarks.Faces, boxesRaw, source); err != nil {
		t.Fatal("valid empty chain", err)
	}
}
