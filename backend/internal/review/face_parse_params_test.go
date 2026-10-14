package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestFaceParseOriginalDetectorInventory(t *testing.T) {
	labelsRaw, err := os.ReadFile("testdata/face-labels19-python.json")
	if err != nil {
		t.Fatal(err)
	}
	detectionRaw, err := os.ReadFile("testdata/face-boxes-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var labels FaceLabels19
	if err := decodeExact(labelsRaw, &labels); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: labels.Image, Canvas: labels.Canvas, TransformChain: labels.TransformChain}
	pin := func(body []byte) ArtifactRef {
		sum := sha256.Sum256(body)
		return ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	detectionRef := pin(detectionRaw)
	boxes, err := ParseFaceBoxes(detectionRaw, source)
	if err != nil {
		t.Fatal(err)
	}
	labels.Faces = detectionRef
	labels.ParsedFaces = len(boxes.Faces)
	labelsRaw, _ = canonical.Marshal(labels)
	labelsRef := pin(labelsRaw)
	params := faceParseParams{Image: source.Image, Faces: detectionRef, ModelID: labels.ModelID, ModelLockID: labels.ModelLockID, PreprocessingID: labels.PreprocessingID, CropSize: 512, Expansion: 1.35}
	body, _ := canonical.Marshal(params)
	ref := pin(body)
	if _, err := VerifyFaceParseParams(body, ref, labelsRaw, labelsRef, detectionRaw, detectionRef, source); err != nil {
		t.Fatal(err)
	}
	params.Expansion = 1.25
	body, _ = canonical.Marshal(params)
	ref = pin(body)
	if _, err := VerifyFaceParseParams(body, ref, labelsRaw, labelsRef, detectionRaw, detectionRef, source); err == nil {
		t.Fatal("different preprocessing accepted")
	}
	mask := ProtectionMask{Artifact: labels.Labels, Canvas: labels.Canvas, TransformChain: labels.TransformChain, Selector: "skin", Confidence: labels.MeanConfidence, ModelLockID: &labels.ModelLockID}
	if err := VerifyFaceSkinConfidence(mask, labels, 1); err != nil {
		t.Fatal(err)
	}
	if VerifyFaceSkinConfidence(mask, labels, 0) == nil {
		t.Fatal("nonzero confidence for empty skin accepted")
	}
	mask.Confidence = 0
	if err := VerifyFaceSkinConfidence(mask, labels, 0); err != nil {
		t.Fatal(err)
	}
	if VerifyFaceSkinConfidence(mask, labels, 1) == nil {
		t.Fatal("zero confidence for nonempty skin accepted")
	}
}
