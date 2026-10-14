package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// PublishSemanticSnapshot persists an immutable, structurally verified snapshot
// through accepted-version reservations. The caller must verify live inventory
// and recipe provenance; publishing does not register or activate a graph.
func (p Publisher) PublishSemanticSnapshot(ctx context.Context, who auth.Identity, tenantID, assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	body := append([]byte(nil), raw...)
	sum := sha256.Sum256(body)
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(v2input.Ref{ArtifactID: documentID, SHA256: hex.EncodeToString(sum[:])}, body)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	found := false
	for _, item := range snapshot.Items {
		if item.Binding.AssetVersionID == assetVersionID {
			found = true
		}
	}
	if !found {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.publish(ctx, who, tenantID, assetVersionID, documentID, body, false, true)
}
