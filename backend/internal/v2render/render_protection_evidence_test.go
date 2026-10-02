package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestRenderProtectionEvidencePinsBytesCanvasAndProtection(t *testing.T) {
	sha := strings.Repeat("a", 64)
	canvas := review.Canvas{Width: 2000, Height: 1000, Orientation: 1, SpaceID: "source", SourceSHA256: sha}
	chainHash, _ := canonical.Hash(map[string]any{"steps": []any{}})
	chain, _ := canonical.Marshal(map[string]any{"steps": []any{}, "hash": chainHash})
	canvasHash, _ := canonical.Hash(canvas)
	evidence := CompiledRenderEvidence{Source: review.WorkingImage{Canvas: canvas, TransformChain: chain}}
	evidence.Recipe.Scalars.Source.CanvasHash = canvasHash
	mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: sha}, Canvas: canvas, TransformChain: chain, Selector: "manual", Confidence: 1, Protected: true}
	pin := func(value review.MaskRef) []byte {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		evidence.Recipe.Recipe, err = canonical.Marshal(map[string]any{"protected_mask": v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := pin(mask)
	if err := ValidateRenderProtectionEvidence(evidence, raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, []byte(`{}`), append(append([]byte(nil), raw...), ' ')} {
		if ValidateRenderProtectionEvidence(evidence, bad) == nil {
			t.Fatal("unpinned bytes accepted")
		}
	}
	mask.Protected = false
	if ValidateRenderProtectionEvidence(evidence, pin(mask)) == nil {
		t.Fatal("unprotected mask accepted")
	}
	mask.Protected = true
	mask.Canvas.Width++
	if ValidateRenderProtectionEvidence(evidence, pin(mask)) == nil {
		t.Fatal("foreign canvas accepted despite matching document SHA")
	}
	mask.Canvas = canvas
	raw = pin(mask)
	evidence.Source.TransformChain = json.RawMessage(`{"steps":[],"hash":"different"}`)
	if ValidateRenderProtectionEvidence(evidence, raw) == nil {
		t.Fatal("foreign source transform accepted")
	}
	evidence.Recipe.Recipe = json.RawMessage(`{"protected_mask":null}`)
	if ValidateRenderProtectionEvidence(evidence, nil) != nil {
		t.Fatal("absent protection rejected")
	}
	if ValidateRenderProtectionEvidence(evidence, raw) == nil {
		t.Fatal("unreferenced protection evidence accepted")
	}
}
