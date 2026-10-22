package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestInteractiveMaskCompilerIdentityAndClosedOperation(t *testing.T) {
	source := styleArtifact{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64), Bytes: 1234}
	pixels := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	var image review.WorkingImage
	image.Canvas.Width = 30
	image.Canvas.Height = 20
	image.Pixels = pixels
	params := artifactjson.Ref{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	declared, _ := json.Marshal([]review.ArtifactRef{source.ref(), pixels})
	lease := InteractiveLease{SourceID: source.ArtifactID, Kind: "mask_prompt", Operation: "mask.prompt", Backend: "cpu", Declared: declared}
	compiled, err := CompileInteractiveMaskMaterialization(lease, source, image, params)
	if err != nil || len(compiled.Inputs) != 2 || len(compiled.Digest) != 64 {
		t.Fatalf("valid current mask materialization: %v", err)
	}
	var profile map[string]any
	if json.Unmarshal(compiled.Profile, &profile) != nil || profile["backend"] != "cpu" {
		t.Fatal("current profile not retained")
	}
	estimates := profile["resource_estimate"].(map[string]any)
	if estimates["pixel_count"] != float64(600) || estimates["output_bytes"] != float64(600*16+8*1048576) {
		t.Fatal("current resource contract changed")
	}
	conflict := pixels
	conflict.SHA256 = strings.Repeat("d", 64)
	lease.Declared, _ = json.Marshal([]review.ArtifactRef{pixels, conflict})
	if _, err = CompileInteractiveMaskMaterialization(lease, source, image, params); err == nil {
		t.Fatal("duplicate identity conflict accepted")
	}
	lease.Declared = declared
	lease.Operation = "image.generate"
	if _, err = CompileInteractiveMaskMaterialization(lease, source, image, params); err == nil {
		t.Fatal("non-mask operation accepted")
	}
	lease.Operation = "mask.prompt"
	lease.SourceID = uuid.New()
	if _, err = CompileInteractiveMaskMaterialization(lease, source, image, params); err == nil {
		t.Fatal("foreign source accepted")
	}
}
