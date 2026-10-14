package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestFrozenRenderDraftFenceValidatesBeforePublication(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
	body, err := BuildFrozenRenderRecipe(authored, ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("d", 64), ModelLockSHA: strings.Repeat("e", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, document, draft := uuid.New(), uuid.New(), uuid.New()
	publisher := Publisher{}
	for _, tc := range []struct {
		draft    uuid.UUID
		revision int64
		body     []byte
	}{
		{uuid.Nil, 1, body}, {document, 1, body}, {draft, 0, body}, {draft, -1, body}, {draft, 1, append(append([]byte(nil), body...), ' ')}, {draft, 1, []byte(`{}`)},
	} {
		if _, err := publisher.PublishFrozenRenderDraftRecipe(context.Background(), who, tenant, binding.AssetVersionID, document, tc.body, tc.draft, tc.revision); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
			t.Fatal("invalid freeze reached upload", err)
		}
		if err := publisher.RegisterFrozenRenderDraftRecipe(context.Background(), who, tenant, binding.AssetVersionID, document, ref, tc.body, tc.draft, tc.revision); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
			t.Fatal("invalid freeze reached SQL", err)
		}
	}
	if _, err := publisher.PublishFrozenRenderDraftRecipe(context.Background(), who, tenant, binding.AssetVersionID, document, body, draft, 1); err == nil || !strings.Contains(err.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("valid freeze did not reach upload", err)
	}
	if err := publisher.RegisterFrozenRenderDraftRecipe(context.Background(), who, tenant, binding.AssetVersionID, document, ref, body, draft, 1); err == nil || !strings.Contains(err.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("valid freeze did not reach SQL", err)
	}
	changed := ref
	changed.SHA256 = strings.Repeat("f", 64)
	if err := publisher.RegisterFrozenRenderDraftRecipe(context.Background(), who, tenant, binding.AssetVersionID, document, changed, body, draft, 1); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatal("changed frozen pin reached SQL", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := publisher.PublishFrozenRenderDraftRecipe(canceled, who, tenant, binding.AssetVersionID, document, body, draft, 1); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
