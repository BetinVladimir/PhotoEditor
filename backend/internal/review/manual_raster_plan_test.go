package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestOriginalManualRasterPlan(t *testing.T) {
	original, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var root rasterizedFaceMask
	if err = decodeExact(original, &root); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: root.Image, Canvas: root.Target.Canvas, TransformChain: root.Target.TransformChain}
	radius, hardness := .1, .5
	vectors := manualVectors{Version: "manual_mask_v1", SourceSHA: source.Canvas.SourceSHA256, Canvas: source.Canvas, Shapes: []manualShape{{Kind: "stroke", Sequence: 4, Mode: "protect", Points: []manualPoint{{X: .5, Y: .5}}, Radius: &radius, Hardness: &hardness}}}
	vectorBody, err := canonical.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	pin := func(body []byte) ArtifactRef {
		sum := sha256.Sum256(body)
		return ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	for _, external := range []bool{false, true} {
		params := manualRasterParams{Image: source.Image, ObjectID: root.Target.ObjectID, Inline: &vectors}
		var externalBody []byte
		if external {
			params.Inline = nil
			ref := pin(vectorBody)
			params.Vectors = &ref
			externalBody = vectorBody
		}
		paramsBody, _ := canonical.Marshal(params)
		paramsRef := pin(paramsBody)
		root.Vectors = paramsRef
		if external {
			root.Vectors = *params.Vectors
		}
		rootBody, _ := canonical.Marshal(root)
		rootRef := pin(rootBody)
		plan, err := ResolveManualRasterPlan(context.Background(), paramsBody, paramsRef, rootBody, rootRef, root.Target, source, externalBody)
		if err != nil || plan.Canvas() != source.Canvas || plan.ShapeCount() != 1 || string(plan.CanonicalVectors()) != string(vectorBody) {
			t.Fatal(plan, err)
		}
		copy := plan.CanonicalVectors()
		copy[0] = '!'
		if string(plan.CanonicalVectors()) != string(vectorBody) {
			t.Fatal("plan is externally mutable")
		}
		bad := append(append([]byte{}, vectorBody...), ' ')
		if denied, err := ResolveManualRasterPlan(context.Background(), paramsBody, paramsRef, rootBody, rootRef, root.Target, source, bad); err == nil || denied.CanonicalVectors() != nil || denied.Canvas() != (Canvas{}) {
			t.Fatal("unbound vectors accepted")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if denied, err := ResolveManualRasterPlan(ctx, paramsBody, paramsRef, rootBody, rootRef, root.Target, source, externalBody); !errors.Is(err, context.Canceled) || denied.CanonicalVectors() != nil {
			t.Fatal("cancelled plan accepted")
		}
	}
}
