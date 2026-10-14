package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestManualOriginalFullOutputInventory(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var document rasterizedFaceMask
	if err := decodeExact(raw, &document); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: document.Image, Canvas: document.Target.Canvas, TransformChain: document.Target.TransformChain}
	digest := sha256.Sum256(raw)
	root := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	outputs, err := ResolveManualFaceMaskOutputs(raw, root, document.Target, source)
	if err != nil || outputs.TargetPNG != document.TargetPNG || outputs.ProtectedPNG != document.ProtectedPNG || outputs.Protected.Artifact != document.Protected.Artifact || !outputs.Protected.Protected {
		t.Fatal(outputs, err)
	}
	if outputs.Target.ObjectID != nil {
		*outputs.Target.ObjectID = uuid.New()
		if *outputs.Target.ObjectID == *document.Target.ObjectID {
			t.Fatal("caller-owned pointer shared")
		}
	}
	for _, alias := range []string{"pngs", "target_png", "protected_png", "root"} {
		forged := document
		switch alias {
		case "pngs":
			forged.ProtectedPNG = forged.TargetPNG
		case "target_png":
			forged.TargetPNG = forged.Target.Artifact
		case "protected_png":
			forged.ProtectedPNG = forged.Protected.Artifact
		case "root":
			forged.TargetPNG.ID = root.ID
		}
		body, err := canonical.Marshal(forged)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		pin := root
		pin.SHA256 = hex.EncodeToString(sum[:])
		denied, err := ResolveManualFaceMaskOutputs(body, pin, document.Target, source)
		if err == nil || denied.TargetPNG.ID != uuid.Nil || denied.Protected.Artifact.ID != uuid.Nil {
			t.Fatal("aliased output accepted", alias, err)
		}
	}
	if denied, err := ResolveManualFaceMaskOutputs(append(append([]byte{}, raw...), ' '), root, document.Target, source); err == nil || denied.TargetPNG.ID != uuid.Nil {
		t.Fatal("changed original bytes accepted")
	}
}
