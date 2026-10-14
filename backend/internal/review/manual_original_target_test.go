package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"testing"
)

func TestOriginalManualTargetResolution(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var document rasterizedFaceMask
	if err := decodeExact(raw, &document); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	root := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	source := DefectSource{Image: document.Image, Canvas: document.Target.Canvas, TransformChain: document.Target.TransformChain}
	target, err := ResolveOriginalManualTarget(raw, root, document.Target.Artifact, source)
	if err != nil || target.Artifact != document.Target.Artifact || target.Selector != "manual" || target.Protected {
		t.Fatal("original manual target not retained", err)
	}
	foreign := document.Target.Artifact
	foreign.ID = uuid.New()
	if _, err := ResolveOriginalManualTarget(raw, root, foreign, source); err == nil {
		t.Fatal("foreign target accepted")
	}
	if _, err := ResolveOriginalManualTarget(append(append([]byte{}, raw...), ' '), root, document.Target.Artifact, source); err == nil {
		t.Fatal("changed pinned root accepted")
	}
	source.Image.ID = uuid.New()
	if _, err := ResolveOriginalManualTarget(raw, root, document.Target.Artifact, source); err == nil {
		t.Fatal("foreign source accepted")
	}
}
