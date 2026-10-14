package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Publishes an accepted-version-scoped protected MaskRef document. Publication
// alone grants no producer/parent provenance or policy authority to consumers.
func (p Publisher) PublishSemanticProtectionDocument(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, binding v2input.SessionVersionBinding, imageBody, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if document == uuid.Nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) < 2 || len(body) > 1048576 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: document, SHA256: hex.EncodeToString(sum[:])}
	if _, err := v2input.ResolveSemanticMaskPixels(ref, body, binding, imageBody, true); err != nil {
		return v2input.Ref{}, err
	}
	return p.publish(ctx, who, tenant, binding.AssetVersionID, document, body, false, true)
}
