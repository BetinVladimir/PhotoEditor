package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestOverrideCurrentAssetRechecksPhysicalTarget(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "revoked", "changed", "foreign_binding", "content_refused"} {
		t.Run(mode, func(t *testing.T) {
			id := uuid.New()
			ref := v2input.Ref{ArtifactID: id, SHA256: rawSHA([]byte("image"))}
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("source"))}, CanvasHash: rawSHA([]byte("canvas"))}
			body, err := canonical.Marshal(map[string]any{"modules": map[string]any{"extensions": map[string]any{"background": map[string]any{"mode": "asset", "asset_id": id.String()}}}})
			if err != nil {
				t.Fatal(err)
			}
			owned := append([]byte(nil), body...)
			witness := &importBackgroundClosureWitnesses{refs: map[uuid.UUID]v2input.Ref{id: ref}}
			if mode == "missing" {
				delete(witness.refs, id)
			}
			cause := errors.New("content rejected")
			content := &importOverrideContentStub{}
			if mode == "content_refused" {
				content.failAt = 1
				content.failure = cause
			}
			calls := 0
			assets := preparedRecipeNodeFunc(func(_ context.Context, entry v2input.Entry) (ImportClosureNode, error) {
				calls++
				if mode == "revoked" {
					return ImportClosureNode{}, cause
				}
				if mode == "changed" {
					witness.mu.Lock()
					witness.refs[id] = v2input.Ref{ArtifactID: id, SHA256: rawSHA([]byte("changed"))}
					witness.mu.Unlock()
				}
				return ImportClosureNode{Entry: entry}, nil
			})
			policy := ImportOverrideCurrentAssetPolicy{Other: content, Binding: binding, Witnesses: witness, Assets: assets}
			actual := binding
			if mode == "foreign_binding" {
				actual.AssetVersionID = uuid.New()
			}
			err = policy.QualifyImportOverrideContent(context.Background(), body, actual)
			if (err == nil) != (mode == "valid") || string(body) != string(owned) {
				t.Fatal("asset policy outcome", mode, err)
			}
			if mode == "valid" && calls != 1 || mode == "foreign_binding" && (calls != 0 || content.calls != 0) {
				t.Fatal("wrong authority order", mode, calls, content.calls)
			}
			if (mode == "revoked" || mode == "content_refused") && !errors.Is(err, cause) {
				t.Fatal("refusal lost", err)
			}
		})
	}
}
