package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestSourceBoundPythonFaceLandmarks(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-landmarks-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var original faceLandmarksDocument
	if decodeExact(raw, &original) != nil {
		t.Fatal("Python collection")
	}
	face := original.Landmarks[0]
	source := DefectSource{Image: original.Image, Canvas: face.Canvas, TransformChain: face.TransformChain}
	digest := sha256.Sum256(raw)
	binding := DefectProposalFaceInput{Landmarks: ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}, ObjectID: &face.ObjectID}
	if err := VerifyDefectProposalFaceDocument(binding, raw, source); err != nil {
		t.Fatal("original proposal face", err)
	}
	missing := uuid.New()
	wrongBinding := binding
	wrongBinding.ObjectID = &missing
	if VerifyDefectProposalFaceDocument(wrongBinding, raw, source) == nil {
		t.Fatal("missing face accepted")
	}
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)-1] ^= 1
	if VerifyDefectProposalFaceDocument(binding, corrupt, source) == nil {
		t.Fatal("changed face bytes accepted")
	}
	mask := ProtectionMask{Artifact: ArtifactRef{ID: uuid.New(), SHA256: binding.Landmarks.SHA256}, Canvas: source.Canvas, TransformChain: source.TransformChain, Selector: "skin", Confidence: 1, ObjectID: &face.ObjectID}
	withMask := binding
	withMask.Skin = &mask
	if err := VerifyDefectProposalFaceDocument(withMask, raw, source); err != nil {
		t.Fatal("source face mask", err)
	}
	foreignMask := mask
	foreignMask.ObjectID = &missing
	withMask.Skin = &foreignMask
	if VerifyDefectProposalFaceDocument(withMask, raw, source) == nil {
		t.Fatal("foreign mask face accepted")
	}
	foreignMask = mask
	foreignMask.Canvas.Width++
	withMask.Skin = &foreignMask
	if VerifyDefectProposalFaceDocument(withMask, raw, source) == nil {
		t.Fatal("foreign mask canvas accepted")
	}
	foreignMask = mask
	foreignMask.Selector = "manual"
	withMask.Skin = &foreignMask
	if VerifyDefectProposalFaceDocument(withMask, raw, source) == nil {
		t.Fatal("wrong mask role accepted")
	}
	selected, err := ResolveFaceLandmarks(raw, source, face.ObjectID)
	if err != nil || selected.ObjectID != face.ObjectID || len(selected.Points) != 68 {
		t.Fatal(selected, err)
	}
	standalone, err := os.ReadFile("testdata/face68-python.json")
	if err != nil {
		t.Fatal(err)
	}
	standaloneDigest := sha256.Sum256(standalone)
	legacyBinding := binding
	legacyBinding.ObjectID = nil
	legacyBinding.Landmarks.SHA256 = hex.EncodeToString(standaloneDigest[:])
	legacyBinding.Skin = &mask
	if err := VerifyDefectProposalFaceDocument(legacyBinding, standalone, source); err != nil {
		t.Fatal("legacy face mask", err)
	}
	foreignMask = mask
	foreignMask.ObjectID = &missing
	legacyBinding.Skin = &foreignMask
	if VerifyDefectProposalFaceDocument(legacyBinding, standalone, source) == nil {
		t.Fatal("legacy foreign mask face accepted")
	}
	if parsed, err := ParseFace68(standalone, source); err != nil || parsed.ObjectID != face.ObjectID {
		t.Fatal(parsed, err)
	}
	cases := map[string]func(*faceLandmarksDocument){
		"wrong image":      func(d *faceLandmarksDocument) { d.Image = d.Faces },
		"foreign canvas":   func(d *faceLandmarksDocument) { d.Landmarks[0].Canvas.Width++ },
		"short points":     func(d *faceLandmarksDocument) { d.Landmarks[0].Points = d.Landmarks[0].Points[:67] },
		"outside point":    func(d *faceLandmarksDocument) { d.Landmarks[0].Points[0].X = 1.1 },
		"bad confidence":   func(d *faceLandmarksDocument) { d.Landmarks[0].Points[0].Confidence = -.1 },
		"duplicate object": func(d *faceLandmarksDocument) { d.Landmarks = append(d.Landmarks, d.Landmarks[0]) },
		"zero object":      func(d *faceLandmarksDocument) { d.Landmarks[0].ObjectID = uuid.Nil },
		"wrong model":      func(d *faceLandmarksDocument) { d.ModelID = "other" },
		"bad model lock":   func(d *faceLandmarksDocument) { d.ModelLockID = "bad" },
		"unknown backend":  func(d *faceLandmarksDocument) { d.Backend = "other" },
		"unknown warning":  func(d *faceLandmarksDocument) { d.Warnings = []string{"other"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var d faceLandmarksDocument
			if decodeExact(raw, &d) != nil {
				t.Fatal("fixture")
			}
			change(&d)
			body, err := canonical.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := ResolveFaceLandmarks(body, source, face.ObjectID); err == nil || got.ObjectID != uuid.Nil {
				t.Fatal("invalid collection accepted", got, err)
			}
		})
	}
	if got, err := ResolveFaceLandmarks(raw, source, uuid.New()); err == nil || got.ObjectID != uuid.Nil {
		t.Fatal("missing object accepted")
	}
	var invalid map[string]any
	json.Unmarshal(standalone, &invalid)
	delete(invalid, "points")
	body, _ := canonical.Marshal(invalid)
	if got, err := ParseFace68(body, source); err == nil || got.ObjectID != uuid.Nil {
		t.Fatal("missing field accepted")
	}
}

func TestLandmarkConfidenceSummaryAndWarnings(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-landmarks-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var original faceLandmarksDocument
	if decodeExact(raw, &original) != nil {
		t.Fatal("fixture")
	}
	source := DefectSource{Image: original.Image, Canvas: original.Landmarks[0].Canvas, TransformChain: original.Landmarks[0].TransformChain}
	cases := map[string]func(*faceLandmarksDocument){
		"forged mean":          func(d *faceLandmarksDocument) { d.MeanConfidence = .8 },
		"false low confidence": func(d *faceLandmarksDocument) { d.Warnings = []string{"low_confidence"} },
		"false no faces":       func(d *faceLandmarksDocument) { d.Warnings = []string{"no_faces"} },
		"missing low confidence": func(d *faceLandmarksDocument) {
			for i := range d.Landmarks[0].Points {
				d.Landmarks[0].Points[i].Confidence = .6
			}
			d.MeanConfidence = .6
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var d faceLandmarksDocument
			if decodeExact(raw, &d) != nil {
				t.Fatal("fixture")
			}
			change(&d)
			body, _ := canonical.Marshal(d)
			if got, err := parseFaceLandmarks(body, source); err == nil || got.Image.ID != uuid.Nil {
				t.Fatal("inconsistent confidence accepted", got, err)
			}
		})
	}
	for _, confidence := range []float64{0, .6, .75, 1} {
		var d faceLandmarksDocument
		if decodeExact(raw, &d) != nil {
			t.Fatal("fixture")
		}
		for i := range d.Landmarks[0].Points {
			d.Landmarks[0].Points[i].Confidence = confidence
		}
		d.MeanConfidence = confidence
		d.Warnings = []string{}
		if confidence < .75 {
			d.Warnings = []string{"low_confidence"}
		}
		body, _ := canonical.Marshal(d)
		if _, err := parseFaceLandmarks(body, source); err != nil {
			t.Fatal("valid confidence", confidence, err)
		}
	}
	original.Landmarks = []Face68{}
	original.MeanConfidence = 0
	original.Warnings = []string{"no_faces"}
	body, _ := canonical.Marshal(original)
	if _, err := parseFaceLandmarks(body, source); err != nil {
		t.Fatal("empty landmarks", err)
	}
	original.Warnings = []string{}
	body, _ = canonical.Marshal(original)
	if _, err := parseFaceLandmarks(body, source); err == nil {
		t.Fatal("missing no_faces accepted")
	}
}

func TestLandmarkConfidenceMatchesNumPyFloat64Mean(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-landmarks-confidence-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc faceLandmarksDocument
	if decodeExact(raw, &doc) != nil {
		t.Fatal("Python confidence fixture")
	}
	source := DefectSource{Image: doc.Image, Canvas: doc.Landmarks[0].Canvas, TransformChain: doc.Landmarks[0].TransformChain}
	if parsed, err := parseFaceLandmarks(raw, source); err != nil || len(parsed.Landmarks) != 8 {
		t.Fatal("NumPy mean rejected", err)
	}
	doc.MeanConfidence += 1e-6
	body, _ := canonical.Marshal(doc)
	if _, err := parseFaceLandmarks(body, source); err == nil {
		t.Fatal("forged mixed mean accepted")
	}
}
