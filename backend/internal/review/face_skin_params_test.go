package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestOriginalSkinParamsPythonParseManifest(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-labels19-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var d FaceLabels19
	if err := decodeExact(raw, &d); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: d.Image, Canvas: d.Canvas, TransformChain: d.TransformChain}
	sum := sha256.Sum256(raw)
	labelsRef := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	params := struct {
		Labels    ArtifactRef `json:"labels"`
		Algorithm string      `json:"algorithm"`
	}{labelsRef, "bisenet_face19_skin_v1"}
	body, _ := canonical.Marshal(params)
	sum = sha256.Sum256(body)
	paramsRef := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	mask := ProtectionMask{Artifact: d.Labels, Canvas: d.Canvas, TransformChain: d.TransformChain, Selector: "skin", Confidence: .9, ModelLockID: &d.ModelLockID}
	if got, err := VerifyFaceSkinParams(body, paramsRef, raw, labelsRef, mask, source); err != nil || got.ParsedFaces != 1 {
		t.Fatal(got, err)
	}
	mask.Confidence = 0
	if _, err := VerifyFaceSkinParams(body, paramsRef, raw, labelsRef, mask, source); err != nil {
		t.Fatal("empty skin confidence", err)
	}
	mask.Confidence = .8
	if _, err := VerifyFaceSkinParams(body, paramsRef, raw, labelsRef, mask, source); err == nil {
		t.Fatal("arbitrary confidence accepted")
	}
	d.Warnings = []string{"no_faces"}
	changed, _ := canonical.Marshal(d)
	if _, err := ParseFaceLabels19(changed, source); err == nil {
		t.Fatal("false no_faces accepted")
	}
	d.Warnings = []string{}
	d.Backend = "untrusted"
	changed, _ = canonical.Marshal(d)
	if _, err := ParseFaceLabels19(changed, source); err == nil {
		t.Fatal("invalid backend accepted")
	}
}
