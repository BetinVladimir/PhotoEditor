package review

import (
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestOriginalDetectorParamsAndOutputBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-detect-params-python.json")
	if err != nil {
		t.Fatal(err)
	}
	boxesRaw, err := os.ReadFile("testdata/face-boxes-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var original FaceDetectParams
	if decodeExact(raw, &original) != nil {
		t.Fatal("Python Params")
	}
	source := DefectSource{Image: original.Image}
	if err := VerifyFaceDetectParams(proposalRef(raw), raw, boxesRaw, source); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*FaceDetectParams){
		"wrong image":            func(p *FaceDetectParams) { p.Image.ID = uuid.New() },
		"valid other model lock": func(p *FaceDetectParams) { p.ModelLockID = strings.Repeat("d", 64) },
		"long side":              func(p *FaceDetectParams) { p.LongSide = 512 },
		"threshold range":        func(p *FaceDetectParams) { p.ScoreThreshold = .1 },
		"nms range":              func(p *FaceDetectParams) { p.NMSIOU = 0 },
		"max faces":              func(p *FaceDetectParams) { p.MaxFaces = 101 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := original
			change(&p)
			body, _ := canonical.Marshal(p)
			if err := VerifyFaceDetectParams(proposalRef(body), body, boxesRaw, source); err == nil {
				t.Fatal("inconsistent Params accepted")
			}
		})
	}
	if err := VerifyFaceDetectParams(proposalRef(raw), append(append([]byte{}, raw...), ' '), boxesRaw, source); err == nil {
		t.Fatal("original SHA substitution accepted")
	}
	var boxes FaceBoxes
	if decodeExact(boxesRaw, &boxes) != nil {
		t.Fatal("boxes")
	}
	second := boxes.Faces[0]
	second.ObjectID = uuid.New()
	boxes.Faces = append(boxes.Faces, second)
	boxes.AcceptedCount = 2
	boxes.CandidateCount = 2
	twoBoxes, _ := canonical.Marshal(boxes)
	oneFaceParams := original
	oneFaceParams.MaxFaces = 1
	oneFaceBody, _ := canonical.Marshal(oneFaceParams)
	if err := VerifyFaceDetectParams(proposalRef(oneFaceBody), oneFaceBody, twoBoxes, source); err == nil {
		t.Fatal("configured max_faces exceeded")
	}
	boxes.Faces = boxes.Faces[:1]
	boxes.AcceptedCount = 1
	boxes.CandidateCount = 1
	boxes.Faces[0].Confidence = .7
	boxesRaw, _ = canonical.Marshal(boxes)
	if err := VerifyFaceDetectParams(proposalRef(raw), raw, boxesRaw, source); err == nil {
		t.Fatal("below configured score accepted")
	}
}
func TestOriginalLandmarkParamsAndOutputBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/landmark-params-python.json")
	if err != nil {
		t.Fatal(err)
	}
	landmarkRaw, err := os.ReadFile("testdata/face-landmarks-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var original LandmarkParams
	var doc faceLandmarksDocument
	if decodeExact(raw, &original) != nil || decodeExact(landmarkRaw, &doc) != nil {
		t.Fatal("fixtures")
	}
	source := DefectSource{Image: doc.Image, Canvas: doc.Landmarks[0].Canvas, TransformChain: doc.Landmarks[0].TransformChain}
	if err := VerifyLandmarkParams(proposalRef(raw), raw, landmarkRaw, source); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*LandmarkParams){
		"image":         func(p *LandmarkParams) { p.Image.ID = uuid.New() },
		"detection ref": func(p *LandmarkParams) { p.Faces.ID = uuid.New() },
		"model lock":    func(p *LandmarkParams) { p.ModelLockID = strings.Repeat("d", 64) },
		"crop size":     func(p *LandmarkParams) { p.CropSize = 128 },
		"expansion":     func(p *LandmarkParams) { p.Expansion = 1.5 },
		"preprocessing": func(p *LandmarkParams) { p.PreprocessingID = "other" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := original
			change(&p)
			body, _ := canonical.Marshal(p)
			if err := VerifyLandmarkParams(proposalRef(body), body, landmarkRaw, source); err == nil {
				t.Fatal("inconsistent Params accepted")
			}
		})
	}
	wrong := proposalRef(raw)
	wrong.SHA256 = strings.Repeat("a", 64)
	if err := VerifyLandmarkParams(wrong, raw, landmarkRaw, source); err == nil {
		t.Fatal("wrong original SHA accepted")
	}
}
