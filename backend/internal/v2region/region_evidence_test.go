package v2region

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
	"testing"
)

func regionEvidenceFixture(t *testing.T) (RegionPreviewRequest, []byte, v2input.Ref, []byte) {
	t.Helper()
	sha := func(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
	canvas := map[string]any{"width": 2000, "height": 1000, "orientation": 1, "space_id": "source", "source_sha256": regionSHA}
	chainSHA, _ := canonical.Hash(map[string]any{"steps": []any{}})
	source, _ := canonical.Marshal(map[string]any{"pixels": v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}, "alpha": nil, "dtype": "float32", "canvas": canvas, "color": map[string]any{"profile_id": "linear_srgb_v1", "encoding": "linear", "icc_sha256": regionSHA}, "transform_chain": map[string]any{"steps": []any{}, "hash": chainSHA}})
	sourceRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha(source)}
	version := uuid.New()
	canvasSHA, _ := canonical.Hash(canvas)
	recipe := v2learning.FrozenRecipe{SchemaVersion: 1, Source: v2input.SessionVersionBinding{AssetVersionID: version, Source: sourceRef, CanvasHash: canvasSHA}, BaseStyle: v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}, Renderer: "v2.2", ICCLockSHA: regionSHA, ModelLockSHA: regionSHA}
	body, _ := canonical.Marshal(recipe)
	req := RegionPreviewRequest{VersionID: version, Recipe: RegionRecipeRef{uuid.New(), sha(body)}, Rect: RegionRect{0, 0, 1, 1}, MaxEdge: 1024}
	return req, body, sourceRef, source
}

func TestRegionEvidenceRequiresExactSourceAndRecipeBytes(t *testing.T) {
	req, body, sourceRef, source := regionEvidenceFixture(t)
	got, err := CompileRegionEvidence(req, body, sourceRef, source)
	if err != nil || got.Geometry.OutputWidth != 1024 {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	wrong := req
	wrong.VersionID = uuid.New()
	if _, err := CompileRegionEvidence(wrong, body, sourceRef, source); err == nil {
		t.Fatal("foreign version accepted")
	}
	if _, err := CompileRegionEvidence(req, append(body, ' '), sourceRef, source); err == nil {
		t.Fatal("changed recipe bytes accepted")
	}
	substituted := sourceRef
	substituted.ArtifactID = uuid.New()
	if _, err := CompileRegionEvidence(req, body, substituted, source); err == nil {
		t.Fatal("substituted source accepted")
	}
}

const regionSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
